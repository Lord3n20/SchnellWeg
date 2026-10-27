#!/usr/bin/env python3
"""Phase 0: Wie gut deckt der offene gtfs.de-Echtzeitfeed die Aachener Unternehmen ab?

Aufruf: rt_abdeckung.py <gtfs.zip> <realtime.pb> [agency_id ...]
Braucht das Paket gtfs-realtime-bindings. Liest stop_times.txt streamend (nur Fahrten der
gewählten Unternehmen), daher wenig RAM. Gibt pro Unternehmen aus:
  laufend   – Fahrten, die zum Feed-Zeitstempel laut Fahrplan unterwegs sind
  im Feed   – davon mit TripUpdate
  Prognose  – davon mit mindestens einer Abweichung != 0 s (echte Echtzeit, nicht nur Fahrplan-Echo)
"""
import csv
import io
import sys
import zipfile
from collections import Counter, defaultdict
from datetime import datetime
from zoneinfo import ZoneInfo

from google.transit import gtfs_realtime_pb2

TZ = ZoneInfo("Europe/Berlin")
# 115 Aachener Straßenbahn (ASEAG), 195 AVV Handeingabe, 202 Rurtalbus, 261 WestVerkehr
STANDARD = ["115", "195", "202", "261"]


def lies(z, name):
    return csv.DictReader(io.TextIOWrapper(z.open(name), encoding="utf-8-sig"))


def sekunden(hms):
    h, m, s = hms.split(":")
    return int(h) * 3600 + int(m) * 60 + int(s)


def aktive_dienste(z, tag):
    datum, wtag = tag.strftime("%Y%m%d"), tag.strftime("%A").lower()
    aktiv = {
        r["service_id"]
        for r in lies(z, "calendar.txt")
        if r["start_date"] <= datum <= r["end_date"] and r[wtag] == "1"
    }
    for r in lies(z, "calendar_dates.txt"):
        if r["date"] == datum:
            (aktiv.add if r["exception_type"] == "1" else aktiv.discard)(r["service_id"])
    return aktiv


def main():
    zpfad, rtpfad, *agenturen = sys.argv[1:]
    agenturen = set(agenturen or STANDARD)

    feed = gtfs_realtime_pb2.FeedMessage()
    with open(rtpfad, "rb") as f:
        feed.ParseFromString(f.read())
    jetzt = datetime.fromtimestamp(feed.header.timestamp, TZ)
    heute = jetzt.strftime("%Y%m%d")
    tagessek = jetzt.hour * 3600 + jetzt.minute * 60 + jetzt.second

    rt = {}  # trip_id -> hat echte Abweichung
    for e in feed.entity:
        if e.HasField("trip_update"):
            tu = e.trip_update
            if tu.trip.start_date and tu.trip.start_date != heute:
                continue
            abw = any(
                (s.HasField("arrival") and s.arrival.delay) or (s.HasField("departure") and s.departure.delay)
                for s in tu.stop_time_update
            )
            rt[tu.trip.trip_id] = rt.get(tu.trip.trip_id, False) or abw

    with zipfile.ZipFile(zpfad) as z:
        namen = {r["agency_id"]: r["agency_name"] for r in lies(z, "agency.txt")}
        route_ag = {r["route_id"]: r["agency_id"] for r in lies(z, "routes.txt") if r["agency_id"] in agenturen}
        dienste = aktive_dienste(z, jetzt)
        trip_ag = {
            r["trip_id"]: route_ag[r["route_id"]]
            for r in lies(z, "trips.txt")
            if r["route_id"] in route_ag and r["service_id"] in dienste
        }
        spanne = {}  # trip_id -> [erste Abfahrt, letzte Ankunft]
        for r in lies(z, "stop_times.txt"):
            t = r["trip_id"]
            if t not in trip_ag:
                continue
            a, d = sekunden(r["arrival_time"]), sekunden(r["departure_time"])
            s = spanne.get(t)
            if s is None:
                spanne[t] = [d, a]
            else:
                s[0], s[1] = min(s[0], d), max(s[1], a)

    zahl = defaultdict(Counter)
    for t, ag in trip_ag.items():
        zahl[ag]["heute"] += 1
        zahl[ag]["heute im Feed"] += t in rt
        zahl[ag]["heute Prognose"] += rt.get(t, False)
        anf, ende = spanne.get(t, (0, -1))
        if anf <= tagessek <= ende:
            zahl[ag]["laufend"] += 1
            zahl[ag]["im Feed"] += t in rt
            zahl[ag]["Prognose"] += rt.get(t, False)

    print(f"Feed-Zeitstempel {jetzt:%d.%m.%Y %H:%M:%S} · {len(rt)} TripUpdates heute")
    print(f"{'Unternehmen':32} {'laufend':>8} {'im Feed':>8} {'Prognose':>9} │ {'heute':>6} {'im Feed':>8} {'Progn.':>7}")
    for ag in sorted(zahl, key=lambda a: -zahl[a]["heute"]):
        c = zahl[ag]
        pct = lambda x, n: f"{100 * x / n:.0f} %" if n else "–"
        print(
            f"{namen.get(ag, ag)[:32]:32} {c['laufend']:8} {pct(c['im Feed'], c['laufend']):>8} "
            f"{pct(c['Prognose'], c['laufend']):>9} │ {c['heute']:6} {pct(c['heute im Feed'], c['heute']):>8} "
            f"{pct(c['heute Prognose'], c['heute']):>7}"
        )


if __name__ == "__main__":
    main()
