# Plan (Stand 27.10.2026, vor Phase 0)

Grundlage: Spezifikation vom 27.10.2026, read-only erfasster Serverstand, erste Quellenprüfung.
Alle Zahlen hier sind **Schätzungen**; Phase 0 ersetzt sie durch Messungen auf dem OptiPlex.

## 1. Ist-Stand Server (gemessen 27.10., `ha apps stats`, `free -m`)

| Komponente | RAM |
|---|---|
| HA Core | 551 MB |
| Supervisor | 591 MB |
| Spieleserver | 140 MB |
| weitere private Dienste (zusammen) | ~390 MB |
| NPM, SSH, sonstige | nicht erfasst (Stats-Abfrage lieferte nichts) → Phase 0 |
| **Gesamt belegt / „available“** | **1,78 GB / 6,07 GB**, Swap 1,0 von 2,5 GB belegt |

CPU: i7-4770S, 4C/8T. Platte: 340 GB frei. Speicherdruck (PSI) 0.

## 2. Architektur

```
Handy (Flutter)                         OptiPlex / HAOS
  SQLite: Haltestellen, Favoriten,        NPM (vorhanden) ── TLS, gzip, SSE ohne Buffering, access_log off
  gespeicherte Verbindungen,                 │
  PMTiles-Karte (offline)                    ▼
  ──HTTPS + Zugangsschutz (§9)──►  routing-core (Go, 1 Binary)          valhalla (Add-on)
                                    ├ API + SSE (Anfrage-ID, Nachholen)  ├ pedestrian/bicycle/auto
                                    ├ RAPTOR auf mmap-Fahrplan           ├ Höhendaten
                                    ├ Fußwegkanten (mmap)                └ kleiner Tile-Cache
                                    ├ Echtzeit-Overlay + Abgleich
                                    ├ Adresssuche (SQLite FTS5/Trigramm)
                                    ├ Plan B/C, Risiko-Ampel
                                    ├ Verkehrsmeldungen (Phase 7)
                                    └ RAM-Caches mit Obergrenzen
                                         │ parallel, Zeitbudget, Backoff
                                         ▼
                              Transitous · gtfs.de-RT · (DB Timetables) · NL/BE-Feeds
                                          (nur IDs, nie Koordinaten)

  routing-jobs (Add-on, boot: manual, Option `job`): osm | elevation | valhalla | gtfs | footpaths |
  mapping | geocode | pmtiles | validate+switch   ← nacheinander gestartet von HA-Automation
```

**MOTIS-Alternative (Phase-0-Entscheidung):** MOTIS 2 bringt RAPTOR, GTFS-RT, Fuß/Rad/Auto-Routing (OSR),
Geocoder und Tiles in einem Prozess. Erfüllt es auf dem OptiPlex RAM- und Zeitziele, ersetzt es den
selbstgebauten RAPTOR, den Fußweg-Job und evtl. die Adresssuche. Der Go-Kern bleibt dann als dünne Schicht für
SSE-Staffelung, Abgleich externer Quellen, Plan B/C, Ampel und Datenschutz-Filter. Valhalla bliebe für
Auto/Rad-Navigation (deutsche Manöver, `exclude_polygons`). Vorab-Einschätzung: MOTIS braucht für die Region
vermutlich deutlich mehr als 250 MB RAM, weil es Fahrplan und Straßengraph gemeinsam hält – das wird gemessen,
nicht vermutet.

### Ordnerstruktur (Quellcode-Repo)
```
CLAUDE.md, README.md, .gitignore
docs/        plan.md, decisions.md, data-sources.md, messungen/
config/      region.geojson, defaults.yaml, local.example.yaml   (local.yaml gitignored)
server/      Go-Modul: cmd/routing-core, cmd/routing-jobs, internal/{raptor,timetable,footpath,rt,
             sources/{transitous,gtfsrt,...},mapping,geocode,api,sse,risk,cache,memguard}
testdata/    Mini-GTFS (DST, >24:00, Mitternacht, Feiertage), Referenzstrecken (neutral)
app/         Flutter-Projekt
scripts/     build-apk.sh, check-apk.sh, measure/*.sh
.github/workflows/  Images bauen → ghcr.io (fester Tag + Digest)
```
Im Add-on-Repo `server-homeassistant`: `routing-core-app/`, `valhalla-app/`, `routing-jobs-app/`, jeweils mit
`image:` auf das fertige GHCR-Image. Grund: Valhalla/Go auf dem Haswell selbst zu bauen dauert lange und
blockiert CPU (die vorhandenen Apps bauen lokal, das passt hier nicht → Entscheidung D-03).

### Datenablage
`/share/oepnv/releases/<zeitstempel>/…`, `/share/oepnv/current → releases/…` (Symlink, atomar per rename),
`/share/oepnv/previous`, `/share/oepnv/raw/` (Downloads + ETags). Der Kern lädt nach Signal neu (mmap neu öffnen,
alte Mappings erst nach laufenden Anfragen freigeben) → kein API-Ausfall.
**Achtung Backups:** `backup_exclude` wirkt nur auf das eigene `/data` bzw. `addon_config` eines Add-ons, nicht auf
`/share`. Zwei Wege (Frage F-13): (a) `/share/oepnv` und in den HA-Backup-Einstellungen `share` abwählen,
(b) Daten im `addon_config` des Job-Add-ons mit `backup_exclude`, Laufzeit-Add-ons mappen `all_addon_configs:ro`.

## 3. RAM-Schätzung

**Betrieb** (RSS inkl. „heißer“ mmap-Seiten; mmap-Seiten sind Page Cache und vom Kernel verdrängbar)

| Komponente | Schätzung |
|---|---|
| routing-core: Go-Laufzeit, API, Caches (gedeckelt) | 30–50 MB |
| Fahrplan (Region, ~15 000 Halte, ~150 000 Fahrten, ~3 Mio. Halte­zeiten, kompakt) | 40–70 MB mmap |
| Fußwegkanten (1 km: ~0,5 Mio. Kanten × 4–6 Byte) | 2–4 MB |
| Echtzeit-Overlay Region (gefiltert) | 5–15 MB |
| Adressindex (SQLite, ~300 000 Einträge) | 10–30 MB heiß |
| Valhalla-Service (mmap-Tiles, kleiner Cache, 2 Threads) | 80–150 MB |
| **Summe** | **≈ 170–320 MB** → Ziel 250 MB erreichbar, aber knapp; harte Grenze sicher |

Kurzfristige Spitze beim Echtzeit-Abruf: gtfs.de-RT ist bundesweit **27 MB pro Abruf** (HEAD 27.10.) → wird
streamend dekodiert und gefiltert, Ziel < 30 MB zusätzlich.

**Einmal-Jobs** (nacheinander)

| Job | Spitze geschätzt | Laufzeit geschätzt |
|---|---|---|
| OSM: Geofabrik NRW + NL-Limburg + Belgien → `osmium extract` auf Polygon | 0,3–1 GB | 5–15 min |
| Höhendaten laden (≈ 12 Kacheln 1°×1°) | < 50 MB | 1–3 min |
| Valhalla-Tiles alle Profile + Höhe (`concurrency=4`) | **1–2 GB** (Risiko) | 20–60 min |
| GTFS-Filter DELFI (277 MB zip, 3 Durchläufe streamend) | 150–300 MB | 5–10 min |
| Fußwege (Valhalla-Matrix one-to-many, ~15 000 Quellen) | Valhalla 200–400 MB | 15–40 min |
| Mapping / Adressindex / PMTiles-Ausschnitt | je < 300 MB | je 1–10 min |

Valhalla-Tile-Bau ist der einzige Kandidat für die 2-GB-Grenze. Fallback laut Spezifikation: Laptop (am Netzteil),
Upload per SSH/Samba – nur mit Messwert-Begründung.

## 4. Plattenplatz (geschätzt)

| | Größe |
|---|---|
| Rohdaten (OSM ~1,5 GB, GTFS DE 0,28 GB, NL/BE ~0,3 GB, Höhe ~0,3 GB) | ~2,4 GB |
| Aufbereitet (Valhalla-Tiles ~1–1,5 GB, Fahrplan ~70 MB, Fußwege ~5 MB, Adressen ~150 MB, PMTiles ~150 MB) | ~1,5–2 GB |
| Während Umschaltung (neu + aktuell + Rollback) | ~5–6 GB |
| **Spitze gesamt** | **≈ 8–9 GB** von 340 GB frei |

## 5. Antwortzeiten (Schätzung pro Stufe)

| Stufe | Server | Ende-zu-Ende Handy (Mobilfunk) |
|---|---|---|
| Nächste Halte + Valhalla erste/letzte Meile (2 Matrizen parallel) | 20–50 ms | |
| RAPTOR (Pareto: Ankunft/Umstiege/Fußweg, Region) | 20–80 ms (Haswell, Risiko McRAPTOR) | |
| Reiner Fußweg | 10–40 ms (parallel) | |
| **Stufe 1 vorläufig** | ~100 ms | **0,25–0,5 s** (TLS-Sitzung warm, sonst +1 RTT) |
| Stufe 2 Echtzeit + Transitous | 0,5–3 s (RT-Feed 27 MB Download dominiert; wird beim App-Öffnen vorgewärmt) | |
| Stufe 3 Ampel + Plan B/C (RAPTOR ab Umstiegspunkt, wenige ms je Umstieg) | +20–100 ms | |
| **Final** | 1–3,5 s, Deckel 8 s | **1,5–4 s** |
| Adresssuche | 5–20 ms | |

## 6. Erste Quellenprüfung – Kurzfassung (Details: data-sources.md)

1. **ASEAG-Echtzeit – erledigt 27.10.: im offenen Feed (93 % / 80 %), siehe D-09.** Ursprünglich größtes Risiko: Der offene gtfs.de-RT-Feed (CC BY-SA 4.0, ohne Registrierung)
   nennt VRS, VRR u. a., **ASEAG/AVV nicht ausdrücklich** (evtl. über „DELFI-SIRI-Quellen“). Phase 0 misst
   die Abdeckung direkt am Feed (Anteil ASEAG-Fahrten mit Prognose). Fehlt sie, gibt es nach heutigem Stand
   keinen offenen Weg zur ASEAG-Echtzeit – dann Frage an dich (Optionen: AVV/ASEAG direkt anfragen, Mobilithek prüfen).
2. **Transitous** verlangt: Projekt Open Source und nicht kommerziell, User-Agent **mit Kontaktmöglichkeit**,
   ab ~720 Routing-Anfragen/Tag Absprache. Logs: IP, URL (enthält Start/Ziel!), UA, 2 Tage. → Wir schicken nur
   Haltestellen-IDs, nie Koordinaten. Kontakt im UA widerspricht deiner Vorgabe → Frage F-05.
3. **DELFI-GTFS** (opendata-oepnv.de) braucht Registrierung; **gtfs.de** bietet `de_nv`/`de_rv`/`de_fv` frei
   (Lizenz/Betreiber auf der Seite nicht genannt → Impressum/Lizenz prüfen). Frage F-06.
4. **DB:** HAFAS/`db-vendo-client` ist inoffiziell → abgelehnt. DB Timetables (API Marketplace, CC BY 4.0)
   braucht Registrierung → nur, wenn der Vergleich an den Referenzstrecken (v. a. Köln 22:30) zeigt, dass es fehlt.
5. **Autobahn-API:** keine Lizenz/Bedingungen beim Betreiber gefunden, nur Dritt-Doku (bund.dev) → Grenzfall, Phase 7.

## 7. Phasen-Anpassungen (Vorschlag)
- Phase 0 zusätzlich: Abdeckungsmessung ASEAG-Echtzeit **vor** allem anderen, weil davon das Hauptziel abhängt.
- Phase-0-Messungen auf dem OptiPlex nur bei leerem Spieleserver und mit nice/ionice; Valhalla-Probebau
  zuerst nur für die StädteRegion, dann hochrechnen, dann volle Region.

## 8. Risiken
| Risiko | Gegenmaßnahme |
|---|---|
| Keine offene ASEAG-Echtzeit | früh messen, sonst Rückfrage; Fahrplan + Transitous als Rückfall |
| Valhalla-Tile-Bau > 2 GB | `concurrency` senken, Region in Teilen, sonst Laptop |
| RT-Feed 27 MB je Abruf (≈ 3 GB/h bei 30-s-Takt) | nur bei offener App, `If-None-Match`, ein Abruf für alle Anfragen, Mindestabstand 30 s |
| McRAPTOR > 100 ms auf Haswell | Pareto-Kriterien begrenzen, Range-RAPTOR, Zielpruning, Halte-Reihenfolge cache-freundlich |
| Spieler-online-Prüfung | geprüft 27.10.: RCON `list` funktioniert, Status-Ping von außerhalb des Containers nicht → RCON verwenden |

## 9. Offene Fragen → siehe Antwort im Chat (F-01 … F-16)

## 9a. App-Startseite: siehe app-ui.md (Vorgabe 27.10.)

## 10. Festlegungen (27.10.2026)
| Frage | Antwort |
|---|---|
| Zugang | eigene Subdomain über NPM mit Client-Zertifikat (mTLS), kein WireGuard |
| Repo | öffentlich, Name **acwegweiser**, am 27.10. umbenannt in **RWaysTH**, am 27.10. abends in **SchnellWeg** (Image-Namen `schnellweg-*` ab dem nächsten Release); Add-ons in `server-homeassistant` |
| Ticket | Deutschlandsemesterticket bestätigt |
| Handy | Android ohne Google-Dienste |
| Transitous-UA | Link auf das öffentliche Repo als Kontakt |
| Fahrplan | gtfs.de **und** DELFI (Registrierung mit Alias ok; vorher sagen, welche Daten) |
| ASEAG-Echtzeit | keine Anfrage an AVV/ASEAG; fehlt sie, bleibt Fahrplan + Transitous |
| DB | DB Timetables gleich einbauen (Registrierung ok) |
| Region ÖPNV | Aachen-Region komplett lokal **+ alle Regionalzüge DE lokal**; Busse/Rest DE über Transitous (als „extern“ markiert) |
| NL/BE | beide nur über Transitous |
| Auto/Rad | **ganz DE, offline auf dem Handy wie Organic Maps** → Abweichung, siehe D-07 |
| Fahrrad | Profil zwischen normal und Rennrad, Treppen tragbar |
| Auto | niedrigste Priorität, kein Android Auto |
| B+R / P+R | erstmal nicht |
| RWTH | mehrere Gebäude; **RWTH Maps als Quelle** für Gebäudesuche (Lizenz in Phase 0 prüfen) |
| Nachtupdate | 04:00, Spieler-Prüfung per RCON im Heimnetz erlaubt |
| Spike | Testbauten auf dem Server erlaubt (Spieleserver leer, gedrosselt, ≤ 2 GB) |
| Backups | große Daten im `addon_config` des Job-Add-ons mit `backup_exclude` |
| Kalender | ja, Termine mit Ort als Ziel; Losgeh-Wecker **neben** einem vorhandenen HA-Wecker |
| Referenzstrecken | Vorschlag bleibt, wird nach Phase 1 mit echten Fahrten angepasst; persönliche Orte nur in local.yaml |
