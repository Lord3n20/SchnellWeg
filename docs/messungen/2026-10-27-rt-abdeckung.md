# Phase 0 · Abdeckung ASEAG-Echtzeit im offenen gtfs.de-Feed (27.10.2026)

Skript: `scripts/measure/rt_abdeckung.py data/raw/de_nv.zip data/raw/rt.pb` (Laptop, nicht der OptiPlex).
Eingänge: gtfs.de `nv_free/latest.zip` (Stand 24.10., 277 MB), `realtime-free.pb` (28,2 MB, Zeitstempel 12:17:25).

| Unternehmen (agency_id) | laufend um 12:17 | davon im Feed | davon mit Abweichung ≠ 0 s |
|---|---|---|---|
| Aachener Straßenbahn = ASEAG (115) | 178 | 93 % | 80 % |
| WestVerkehr (261) | 40 | 92 % | 75 % |
| Rurtalbus (202) | 54 | 80 % | 57 % |
| AVV Handeingabe (195) | 3 | 0 % | 0 % |

- **Ergebnis: ASEAG-Echtzeit ist im offenen Feed enthalten.** Das größte Risiko aus plan.md §6/§8 entfällt.
- Fahrt-IDs im Feed passen direkt zu `trips.txt` des statischen gtfs.de-Feeds (kein Mapping nötig).
- Der Feed enthält nur Fahrten um die aktuelle Zeit herum (ganzer Tag: 38 % ASEAG im Feed), passt für Abfragen „jetzt“.
- Kosten der Auswertung (Python, Laptop): 52 s, 200 MB RSS – fast alles fürs Durchlesen von stop_times (2,1 GB entpackt).
  Im Betrieb wird nur der RT-Feed dekodiert; Messung auf dem OptiPlex mit Go folgt.
- Nur eine Stichprobe (Dienstag mittag). Wiederholen: Abend, Wochenende, Störungsfall.

## Wiederholung Dienstag abends (21:40)

`realtime-free.pb` 13,3 MB (mittags 28,2 MB), Zeitstempel 21:40:55, 42 353 TripUpdates.

| Unternehmen (agency_id) | laufend um 21:40 | davon im Feed | davon mit Abweichung ≠ 0 s |
|---|---|---|---|
| ASEAG (115) | 70 | 89 % | 57 % |
| WestVerkehr (261) | 13 | 100 % | 69 % |
| Rurtalbus (202) | 17 | 88 % | 41 % |
| AVV Handeingabe (195) | 16 | 0 % | 0 % |

- Abdeckung abends fast wie mittags (89 statt 93 %); weniger Fahrten mit Abweichung (57 statt 80 %) – abends
  fahren die Busse eher pünktlich, eine Fahrt im Feed ohne Abweichung ist also kein Ausfall der Echtzeit.
- AVV-Handeingabe (Nachtbusse/Sonderverkehre, abends 16 Fahrten) hat nie Echtzeit → in der App als
  „nur Fahrplan“ kennzeichnen.
- Offen: Wochenende, Störungsfall.
