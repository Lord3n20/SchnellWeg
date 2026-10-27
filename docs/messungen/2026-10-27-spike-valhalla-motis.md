# Phase 0 · Spike Valhalla + MOTIS auf dem OptiPlex (27.10.2026, 14:20–14:50)

Wegwerf-Container auf dem Server (`docker run --rm --memory 2g --cpus 4`, Jobs zusätzlich `nice -n 19 ionice -c3`),
nacheinander, Spieleserver leer (per RCON geprüft). Skripte: `scripts/measure/spike/`.
Eingänge: Geofabrik `koeln-regbez-latest.osm.pbf` (215 MB, md5 ok), gtfs.de `nv_free/latest.zip` (265 MB).
Box StädteRegion `5.95,50.48,6.45,50.93` (etwas NL/BE-Rand dabei). „Spitze“ = cgroup `memory.peak`
(enthält Page Cache, also obere Schranke), RSS = VmHWM bzw. `time -f %M`.

## Vorbereitung

| Schritt | Wand | Spitze | Ergebnis |
|---|---|---|---|
| `osmium extract -s smart` (Box) | – | **OOM bei 2 GB** | abgebrochen |
| `osmium extract -s complete_ways` | – | **OOM bei 2 GB** | abgebrochen |
| `osmium extract -s simple` | 3 s | ~2,0 GB RSS | 33 MB, aber unvollständige Wege (MOTIS: „invalid location“) |
| `osmconvert --complete-ways --hash-memory=400` | **10 s** | 0,8 GB RSS / 1,15 GB cgroup | 35 MB ✅ |
| `gtfs_bbox.py` (de_nv → Box, 2× streamend) | 72 s | **22 MB RSS** | 15 388 Fahrten, 7 119 Halte, 279 Linien, 3,3 MB |

Folge für den OSM-Job: osmium hält ID-Mengen über den ganzen ID-Raum (~2 GB, unabhängig von der Regionsgröße)
→ für den Job `osmconvert` mit begrenztem Hash (oder fertige Geofabrik-Ausschnitte) statt osmium.

## Valhalla 3.9.1 (`ghcr.io/valhalla/valhalla:3.9.1@sha256:4fd114eb1d26b8fc2f43bcb3df234c1b63a0bf13c277405e5943d5361dc575de`)

Profile Fuß/Rad/Auto, ohne Höhe, `concurrency=4`. Dienst `valhalla_service … 2` (2 Threads), Standard-Cache.

| | StädteRegion |
|---|---|
| admins / timezones / tiles / extract | 1 s / 7 s / **8 s** / 0 s |
| Tiles | 29 MB (+ tz.sqlite 117 MB, admin 7 MB) |
| Bau-Spitze (cgroup) | **1,16 GB** (inkl. Page Cache) |
| Dienst RSS nach Start / nach Messung | 72 MB / **201 MB** |
| Route Fuß (7 Paare × 3) | Median 34 ms, p95 62 ms |
| Route Rad | Median 38 ms, p95 69 ms |
| Route Auto | Median 41 ms, p95 82 ms |
| Matrix 1×30 Fuß im 1-km-Umkreis (8 Orte × 3) | Median **58 ms**, p95 **101 ms** |

Erster Lauf auf dem unvollständigen `simple`-Ausschnitt: Bau-Spitze 0,67 GB, Fuß Median 22 ms, Matrix Median 53 ms
(nicht vergleichbar, nur als Streuung).

Bewertung: Erste/letzte Meile (Matrix) verfehlt das Ziel 50 ms und kratzt an der harten Grenze 100 ms (p95).
Dienst-RSS 200 MB liegt über der Schätzung (80–150 MB) → Phase 1: `mjolnir.max_cache_size` senken,
Matrix-Ziele vorfiltern (Luftlinie), `costmatrix` vs. `timedistancematrix` vergleichen, Fußwege zwischen
Halten vorab berechnen statt live.

### Hochrechnung: ganzer Regierungsbezirk Köln (215 MB PBF, ≈ 6× StädteRegion)

| | Regbez. Köln |
|---|---|
| admins / timezones / tiles / extract | 3 s / 7 s / **49 s** / 1 s |
| Tiles | 194 MB |
| Bau-Spitze (cgroup) | **2,05 GB = Limit erreicht, kein OOM** → Kernel hat Page Cache verdrängt; echte anonyme Spitze < 2 GB, aber nicht genau bekannt |
| Dienst RSS nach Messung | 216 MB |
| Route Fuß / Rad / Auto | Median 30 / 43 / 57 ms, p95 66 / 89 / 85 ms |
| Matrix 1×30 Fuß | Median **49 ms**, p95 **99 ms** |

- Bauzeit wächst etwa linear (8 s → 49 s); volle Region (Regbez. Köln + Düren/Heinsberg-Rand + Süd-Limburg +
  Ostbelgien + Lüttich, grob 1,5–2× Köln) → geschätzt 1,5–2 min, Tiles ~300–400 MB.
- RAM: anonyme Spitze muss im Job gemessen werden (`memory.stat` anon statt `memory.peak`); bei Engpass
  `concurrency` 4 → 2. Laptop-Ausweichweg derzeit **nicht nötig**.
- Latenzen ändern sich mit der Regionsgröße kaum; Dienst-RSS wächst nur leicht (201 → 216 MB).

## MOTIS v2.11.3 (statisches Release-Binary, debian:13-slim)

| Variante | Import | Spitze | Daten | Server-RSS | Abfrage |
|---|---|---|---|---|---|
| OSM (StädteRegion 35 MB) + GTFS, ohne Kartenkacheln | – | **OOM bei 2 GB nach 4 s** in OSR „Load OSM / Ways“ | – | – | – |
| nur GTFS (`street_routing/geocoding: false`) | **0,6 s** | 89 MB RSS / 171 MB cgroup | 12 MB | **64 MB** | Halt→Halt Median **2,8 ms**, p95 18 ms |

- Der OSR-Import (Straßengraph) sprengt die 2-GB-Grenze schon bei der kleinsten Region; wie osmium skaliert er
  vermutlich mit dem OSM-ID-Raum, nicht mit der Fläche → volle Region auf dem Server aussichtslos.
- Laptop-Import + Upload war geplant zum Messen der echten Spitze, Ausführen des MOTIS-Binaries auf dem Laptop
  wurde vom Auto-Modus blockiert → offen.
- Nur-Fahrplan-Kern ist schlank und schnell, findet ohne Fußwege zwischen Halten aber nur 9 von 21 Verbindungen
  (z. B. Hbf → Elisenbrunnen: keine, weil der nächstgelegene Halt ein Bahnsteig ohne Umstiegskante ist).
  Fußwege müssten wir selbst (Valhalla) als `transfers.txt` einspeisen; Zugang mit mehreren Start-Halten und
  Gehzeit-Offsets bietet die `plan`-API ohne OSR nicht.

## Folgen für D-02
Siehe decisions.md D-02 (Stand 27.10. abends).
