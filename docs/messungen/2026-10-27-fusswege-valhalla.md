# Phase 0 · Fußwege und erste/letzte Meile mit Valhalla (27.10.2026, 16:33–16:40)

Wegwerf-Container auf dem Server (`docker run --rm --memory 2g --cpus 4`, `nice -n 19 ionice -c3`), Spieleserver leer
(keine Anmeldung in den letzten 6 h laut Server-Log). Skripte: `scripts/measure/spike/` (`run-footpaths.sh`,
`footpaths.py`, `bench.py access`). Eingänge: Geofabrik `koeln-regbez-latest.osm.pbf` (215 MB, md5 ok),
Box StädteRegion `5.95,50.48,6.45,50.93`, Fahrplan sr + de_rv + de_fv für den 28.10.

Übertragung Laptop → Server: Kandidatenpaare (142 KB gzip) über ein Entwurfs-Release im privaten Repo und eine signierte
Kurzzeit-URL, Release danach gelöscht. OSM lädt der Server selbst. Zurück: Ergebnis-CSV über das HA-Terminal (gzip|base64).

## Bau (wie Spike, zur Kontrolle)

| Schritt | Wand | Spitze |
|---|---|---|
| Download 215 MB | 61 s | – |
| `osmconvert` Box | 17 s inkl. `apt-get install osmctools` | – |
| Valhalla admins + tiles + extract (ohne Zeitzonen) | **9 s** | 0,97 GB cgroup (inkl. Page Cache) |

## Fußwege zwischen Halten (Job `footpaths`, Prototyp)

Kandidaten: `raptorprobe -pairs-out` = Luftlinie ≤ 400 m zwischen Halten mit Abfahrten, Quelle in der Box.
Valhalla `sources_to_targets` one-to-many je Quelle, `pedestrian`, `walking_speed` 4,3 km/h (D-12).

| | Ergebnis |
|---|---|
| Quellen / Paare | 3 349 / 13 989 |
| mit Weg | 13 981 (8 ohne: vier Halte in zwei Gruppen, die Valhalla nicht verbindet) |
| Laufzeit, 2 Client-Threads / 4 | **7,0–7,2 s** / 5,3 s (5,1 km/h-Lauf) |
| je Anfrage Median / p95 | 3,3 ms / 8,1 ms |
| Dienst VmHWM | 229–231 MB |
| Umweg echter Weg / Luftlinie (Luftlinie ≥ 50 m) | Median 1,23, p95 2,16, max 24,7 |
| Wege > 800 m (entfallen, D-12) | 167 |

Mit 5,1 km/h (Valhalla-Standard) war die Gehzeit im Median 184 s gegenüber 230 s beim Luftlinien-Platzhalter
(× 1,3 bei 1,2 m/s). Der Platzhalter-Faktor 1,3 liegt nah am gemessenen Median (1,23), unterschätzt aber die
Ausreißer (Gleise, Autobahn, Wasser).

### Wirkung auf RAPTOR (Laptop, Akku, beide Läufe gleich)

| | Fußwege | Wege geprüft | Anfrage Median / p95 |
|---|---|---|---|
| Luftlinie | 23 342 | 51 ok | 3,5 / 6,0 ms |
| Valhalla (Box), Luftlinie außerhalb | 23 179 | 50 ok | 3,5 / 6,1 ms |

Von den Referenzverbindungen ändert sich eine (Hbf → Elisenbrunnen 00:35: Ausstieg Alter Posthof statt
Elisenbrunnen, gleiche Ankunft). Auffällig: Umstiegsfußweg direkt vor dem Fußweg zum Ziel → in der Ausgabe
zusammenfassen bzw. bei gleicher Ankunft den direkten Weg vorziehen (Phase 1).

Hochrechnung volle Region (≈ 10× Halte): ~70 s Valhalla-Anfragen, unkritisch; Job-Spitze = Valhalla-Dienst (~230 MB).

## Erste/letzte Meile (Matrix 1×N auf echte Zugangshalte)

`raptorprobe -access-out`: Halte mit Abfahrten ≤ 800 m Luftlinie um die 8 neutralen Orte in der Box
(22–71 Ziele, Elisenbrunnen am meisten). Je Ort 5 Wiederholungen, n=40, zwei Durchläufe, Dienst mit 2 Threads.

| Variante | Median | p95 | max |
|---|---|---|---|
| alle Halte | 28,6–30,2 ms | 75–78 ms | 79–94 ms |
| `verbose: false` | 26,7–33,9 ms | 64–68 ms | 73–81 ms |
| Bahnsteige im 25-m-Raster zusammengefasst (17–66 Ziele) | 26,7–35,5 ms | 69–74 ms | 72–78 ms |

Dienst VmHWM 150 MB (nur Matrix-Anfragen).

### Ausgedünnt: nur der nächste Halt je Muster (`raptorprobe -per-route`, 16:45)

| Variante | Ziele | Median | p95 | max |
|---|---|---|---|---|
| alle Halte | 22–71 | 28,7–29,3 ms | 59–69 ms | 74–84 ms |
| je Muster nächster Halt | **5–31** | **17,7–20,0 ms** | **58,6–58,8 ms** | 64–68 ms |

- RAPTOR-Ergebnisse unverändert: alle Ankunftszeiten der 8 Paare × 5 Uhrzeiten gleich, 52/52 Wege ok.
- Aufgeschlüsselt nach Ort und Wiederholung: kein Kaltstart-Effekt. Stolberg und SuperC liegen bei jedem Lauf bei
  50–60 ms, die übrigen Orte bei 4–30 ms. Keine unerreichbaren Ziele; die längsten Ziele sind 0,9–1,3 km echter
  Weg bei ≤ 800 m Luftlinie – die Matrix muss so weit suchen. Hebel Phase 1: Radius nach Halt-Dichte
  (z. B. 600 m in der Stadt) oder Zeitgrenze für den Zugang.
- Dabei behoben: aufeinanderfolgende Fußwege (Zugang + Umstieg, Umstieg + Weg zum Ziel) werden zu einem
  zusammengefasst (`mergeWalks`).

Bewertung: **Median-Ziel 50 ms erreicht, p95 unter der harten Grenze 100 ms, p95-Ziel (50 ms) verfehlt.** Kompakte
Antwort und Raster bringen wenig; Ausdünnen je Muster senkt den Median deutlich (→ übernehmen), das p95 nicht.
Für Phase 1: Radius/Zeitgrenze (s. o.), Matrix parallel zum Adress-Lookup starten, `mjolnir.max_cache_size` für RAM.
