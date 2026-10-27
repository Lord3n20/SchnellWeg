# Wege auf der Karte: Fußwegformen, Halte je Fahrt, reiner Fußweg (D-19, 27.10.2026 spät)

routing-core 0.1.12 auf dem Server (Mindesttakt 2 GHz, Valhalla 2 Threads). Messung von außen über den Reverse
Proxy mit Client-Zertifikat, 8 Paare neutraler Orte (Hbf, Elisenbrunnen, SuperC, Bushof, Uniklinik, Westbahnhof,
Rothe Erde, Ponttor) × 4 Uhrzeiten (07:30, 12:00, 17:15, 21:00) × 3 = 96 Anfragen, nacheinander.

| Messgröße | Median | p95 | max | Ziel / Grenze |
|---|---|---|---|---|
| `walk_ms` (Zugang + Abgang, reiner Fußweg parallel) | 32,3 ms | 43,0 ms | 44,2 ms | < 50 / < 100 ms ✓ |
| `shape_ms` (Wegformen der ausgegebenen Fußabschnitte) | 13,9 ms | 26,7 ms | 34,1 ms | – |
| Anfrage gesamt (Laptop, über Internet) | 169 ms | 209 ms | 234 ms | – |

- Vorher (0.1.5–0.1.11, 2 GHz): `walk_ms` 31,2 / 37,5 ms. Der reine Fußweg läuft gleichzeitig mit den beiden
  Matrizen auf denselben 2 Valhalla-Threads; p95 steigt dadurch um ~5 ms, bleibt unter 50 ms.
- Die Wegformen kommen nach RAPTOR dazu (nur für die ausgegebenen Verbindungen, gleiche Wege einmal, parallel).
- Vollständigkeit: 366/366 Fußabschnitte mit Form, 186/186 Fahrten mit Haltefolge, 48 reine Fußwege
  (die 3 Paare über 3 km Luftlinie ohne). Stichprobe Hbf → SuperC: Form beginnt/endet an Start und Ziel,
  reiner Fußweg 1,8 km / 25 min.
- App mit Zwischenhalten und Fußwegformen installiert; Ansicht auf dem Handy prüft der Betreiber.
