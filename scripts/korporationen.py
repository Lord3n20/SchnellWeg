#!/usr/bin/env python3
"""Baut app/assets/korporationen.json aus der Couleurbummel-Liste (D-50).

Quelle: CSV „wikipedia-corporations-cleaned_V*.csv“ aus github.com/heinstalator/couleurbummel_database_failback
(Releases), aus Wikipedia-Listen erzeugt, nicht geprüft. Übernommen werden nur Häuser mit Anschrift oder Koordinaten
in config/region.geojson. Fehlen Koordinaten, sucht der eigene Server die Anschrift (/v1/search, mTLS); nur ein
Treffer der Art „adresse“ mit derselben Postleitzahl zählt. Kennt er sie nicht (Adressdatenbank lückenhaft), fragt das
Skript einmalig Nominatim (OSM, ODbL; nur die Anschrift des Hauses, 1 Anfrage/s). Was sich nicht finden lässt, wird
aufgelistet und fällt weg.

Aufruf: scripts/korporationen.py <csv> <basis-url> <cert.pem> <key.pem>
"""
import csv
import json
import ssl
import sys
import urllib.request
from pathlib import Path

WURZEL = Path(__file__).resolve().parent.parent
FARBEN = {0: "unbekannt", 1: "keine Farben", 2: "nadeltragend", 3: "farbenführend", 4: "fakultativ farbentragend", 5: "farbentragend"}
FECHTEN = {0: "unbekannt", 1: "nichtschlagend", 2: "frei schlagend", 3: "fakultativ schlagend", 4: "pflichtschlagend"}


def region():
    g = json.loads((WURZEL / "config/region.geojson").read_text())
    f = g["features"][0] if "features" in g else g
    geo = f["geometry"]
    ringe = [geo["coordinates"][0]] if geo["type"] == "Polygon" else [p[0] for p in geo["coordinates"]]
    return ringe


def innen(lon, lat, ringe):
    for ring in ringe:
        drin = False
        for (x1, y1), (x2, y2) in zip(ring, ring[1:] + ring[:1]):
            if (y1 > lat) != (y2 > lat) and lon < (x2 - x1) * (lat - y1) / (y2 - y1) + x1:
                drin = not drin
        if drin:
            return True
    return False


def suche(basis, ctx, q):
    req = urllib.request.Request(basis.rstrip("/") + "/v1/search", data=json.dumps({"q": q, "n": 3}).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
        return json.load(r).get("hits") or []


def nominatim(strasse, plz, stadt):
    import time
    import urllib.parse
    time.sleep(1.1)  # Nutzungsregeln: höchstens 1 Anfrage je Sekunde
    q = urllib.parse.urlencode({"street": strasse, "postalcode": plz, "city": stadt, "country": "de", "format": "jsonv2", "limit": 1})
    req = urllib.request.Request("https://nominatim.openstreetmap.org/search?" + q, headers={"User-Agent": "SchnellWeg-Datenaufbereitung/1"})
    with urllib.request.urlopen(req, timeout=15) as r:
        h = json.load(r)
    return (float(h[0]["lat"]), float(h[0]["lon"])) if h else None


def zahl(s, sonst=0):
    try:
        return int(s)
    except ValueError:
        return sonst


def main():
    pfad, basis, cert, key = sys.argv[1:5]
    ctx = ssl.create_default_context()
    ctx.load_cert_chain(cert, key)
    ringe = region()
    xs = [p[0] for r in ringe for p in r]
    ys = [p[1] for r in ringe for p in r]
    grob = (min(xs), max(xs), min(ys), max(ys))
    aus, fehlt = [], []
    for z in csv.DictReader(open(pfad, encoding="utf-8")):
        if z.get("hidden", "").strip().lower() in ("1", "true"):
            continue
        strasse, plz, stadt = z["street"].strip(), z["postalCode"].strip(), z["city"].strip()
        lat, lon = z["latitude"].strip(), z["longitude"].strip()
        if lat and lon:
            lat, lon = float(lat), float(lon)
        elif strasse and plz:
            # Grob vorsortieren: deutsche PLZ der Region beginnen mit 4/5, Belgien/NL fehlen in der Liste
            if z["country"] != "DE" or plz[0] not in "45":
                continue
            treffer = [h for h in suche(basis, ctx, f"{strasse}, {plz}") if h["kind"] == "adresse" and plz in h.get("detail", "")]
            if treffer:
                lat, lon = treffer[0]["lat"], treffer[0]["lon"]
            elif (n := nominatim(strasse, plz, stadt)) is not None:
                lat, lon = n
            else:
                fehlt.append(f"{z['shortName']}: {strasse}, {plz} {stadt}")
                continue
        else:
            continue
        if not (grob[0] <= lon <= grob[1] and grob[2] <= lat <= grob[3] and innen(lon, lat, ringe)):
            continue
        e = {"n": z["shortName"].strip(), "lat": round(lat, 6), "lon": round(lon, 6)}
        if z["longName"].strip() and z["longName"].strip() != e["n"]:
            e["l"] = z["longName"].strip()
        if strasse:
            e["a"] = f"{strasse}, {plz} {stadt}".strip(", ")
        for k, q in (("v", "organisations"), ("f", "colours"), ("g", "foundationDate"), ("w", "website"), ("j", "jourFixe"), ("m", "motto")):
            if z[q].strip():
                e[k] = z[q].strip()
        if zahl(z["coloursPrinciple"]):
            e["fp"] = FARBEN.get(zahl(z["coloursPrinciple"]), "")
        if zahl(z["fencingPrinciple"]):
            e["sp"] = FECHTEN.get(zahl(z["fencingPrinciple"]), "")
        aus.append(e)
    aus.sort(key=lambda e: (e["lat"], e["lon"]))
    ziel = WURZEL / "app/assets/korporationen.json"
    ziel.parent.mkdir(exist_ok=True)
    ziel.write_text(json.dumps({"quelle": "Wikipedia (CC BY-SA 4.0) über Couleurbummel-Liste V9, ungeprüft", "eintraege": aus},
                               ensure_ascii=False, separators=(",", ":")) + "\n", encoding="utf-8")
    print(f"{len(aus)} Häuser in der Region → {ziel.relative_to(WURZEL)}")
    if fehlt:
        print(f"{len(fehlt)} Anschriften nicht gefunden:")
        print("\n".join("  " + f for f in fehlt))


if __name__ == "__main__":
    main()
