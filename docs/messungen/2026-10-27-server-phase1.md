# Phase 1 · Server komplett auf dem OptiPlex (27.10.2026, 22:20–22:30)

routing-core 0.1.10 + routing-jobs 0.1.7, Messung von außen über die Domain mit Client-Zertifikat (Laptop, WLAN).

| Messgröße | Wert | Ziel / Grenze |
|---|---|---|
| Job `geocode` (Box mit NL/BE-Rand) | 345 642 Einträge (309 791 Adressen, 14 322 Straßen, 563 Orte, 20 966 Einrichtungen), 20 s, Spitze 235 MB | ≤ 2 GB |
| Job `map` (Protomaps-Build 20261027, Zoom ≤ 15) | 49 MB PMTiles, 25 s, Spitze 109 MB, `pmtiles verify` ok | ≤ 2 GB |
| Suche im Server (`ms` der Antwort) | superc 0,03 · pontstr 5 0,17 · vaals 1,2 · aachen 6,0 · 52062 8,5 ms | < 30 / 50 ms |
| Abfahrten, erste Anfrage (inkl. Echtzeit-Abruf) | 0,56 s gesamt | Quelle ≤ 6 s |
| Echtzeitstand | 5 381 Fahrten der Region mit Prognose (Feed 22:27:25) | – |
| `/v1/stops` | 673 KB, 1,1 s | – |
| RAM routing-core (docker stats) | 97 MB vor, 121 MB nach dem ersten Echtzeit-Abruf (Heap nach Laden 93 MB, davon Suchliste ~40 MB) | – |
| RAM Valhalla | 44 MB Leerlauf, unter Last bis ~190 MB anonym (Phase 0) | – |
| **RAM neue Add-ons gesamt** | ~165 MB Leerlauf, unter Last geschätzt ~310 MB | **Ziel < 250 MB verfehlt**, Grenze < 500 MB ok |

- Ziel 250 MB wird unter Last überschritten: Haupttreiber Suchliste (~40 MB) und Echtzeit-Puffer (~25 MB) in
  routing-core sowie Valhalla. Möglichkeiten: Suchliste ohne NL/BE-Rand oder kompakter (Strings in einem Block),
  GOMEMLIMIT routing-core von 200 auf 120 MiB, Valhalla-Cache kleiner. Entscheidung beim Betreiber.
- Fehler beim ersten Ausrollen (0.1.9): Echtzeit ohne CA-Zertifikate im leeren Image, Suche „vaals“ 63 ms → 0.1.10.
