# Architekturentscheidungen

Format: Entscheidung · Begründung · verworfene Alternativen · Messwerte · Status
(offen = wartet auf Phase-0-Messung oder auf den Betreiber).

## D-01 Sprache Kern-Server: Go
- Begründung: Vorgabe der Spezifikation; ein statisches Binary, `GOMEMLIMIT`, mmap über `syscall.Mmap`, schnelle
  Builds im CI. Rust wäre bei RAM etwas sparsamer, der Unterschied ist bei ~50 MB Heap aber klein.
- Verworfen: Rust (kein messbarer Vorteil erwartet), Python (zu langsam für RAPTOR auf Haswell).
- Status: angenommen, durch RAPTOR-Messung bestätigt (27.10.).

## D-02 Eigenes RAPTOR vs. lokales MOTIS
- Status: **angenommen (27.10.): eigenes RAPTOR.** Kriterien waren: RAM im Betrieb (Ziel < 250 MB gesamt), Antwortzeit auf dem OptiPlex
  (< 100 ms), Echtzeit-Overlay, Steuerbarkeit für Plan B/C und Ampel, RAM-Spitze beim Import (≤ 2 GB).
  Vorgabe: Reife fremde Engine geht vor, wenn sie die Zielwerte erfüllt.
- Messung 27.10. (messungen/2026-10-27-spike-valhalla-motis.md): MOTIS v2.11.3 mit Straßengraph (OSR)
  **OOM bei 2 GB schon für die StädteRegion** (Schritt „Load OSM / Ways“, anon-rss 2,08 GB laut Kernel) → verletzt
  die harte Job-Grenze. Nur-Fahrplan-MOTIS: 64 MB, 2,8 ms Median – aber ohne Fußwege/Zugangs-Offsets, die
  dann wir liefern müssten.
- Messung 27.10. (messungen/2026-10-27-raptor.md): eigener RAPTOR-Prototyp (Go, mit Weg-Rekonstruktion und
  Vortagsfahrten) auf dem OptiPlex **0,7–1,1 ms Median, p95 ≤ 2,4 ms, RSS < 25 MB**, Laden 0,5 s – Antwortzeit- und
  RAM-Ziel deutlich erfüllt (Fußwege noch Luftlinie).
- **Entscheidung:** eigenes RAPTOR in Go auf mmap-Fahrplan; Fußwege zwischen Halten vorab mit
  Valhalla (Job `footpaths`), erste/letzte Meile als Valhalla-Matrix → Multi-Source-RAPTOR mit Offsets.
  Begründung: die reife Engine erfüllt die Import-Grenze nicht; ihr schlanker Fahrplankern allein spart uns
  RAPTOR, aber nicht Fußwege/Zugang, und schränkt Plan B/C (Neustart ab Umstiegspunkt mit Offsets) ein.
- Alternative, falls doch MOTIS gewünscht ist: OSR-Import auf dem Laptop (selten, nur bei OSM-Wechsel), Fahrplan-Import
  täglich auf dem Server – vorher Laptop-Messung der echten Spitze.

## D-03 Images im CI bauen (GHCR), nicht auf dem Gerät
- Begründung: Die vorhandenen Apps bauen auf HAOS aus dem Dockerfile. Für Go ginge das, für Valhalla
  (C++, ggf. eigener Build) würde es den Haswell lange auslasten und den Spieleserver stören. Fertige Images mit
  festem Tag + Digest aus dem Quellcode-Repo, im Add-on-Repo per `image:` referenziert.
- Verworfen: Bau auf dem Gerät (CPU-Last), Docker Hub (zusätzlicher Account).
- Abhängig von F-02 (öffentliches Repo → GHCR ohne Login).
- Status: angenommen – Repo seit 27.10.2026 öffentlich, damit ist F-02 erfüllt.

## D-04 Datenablage `/share/oepnv` mit Symlink-Umschaltung
- Status: ersetzt durch D-04 (aktualisiert), Backup-Frage F-13 beantwortet.

## D-05 Echtzeit-Feed gtfs.de: Abruf nur bei offener App
- Feed ist bundesweit 27 MB (HEAD 27.10.2026, ETag vorhanden). Abruf nur bei Bedarf, höchstens alle 30 s,
  bedingt (`If-None-Match`), streamend dekodiert und auf Regions-Trips gefiltert.
- Gemessen 27.10. (messungen/2026-10-27-rt-optiplex.md): Abruf+Filter 0,8–0,9 s, Filter allein 33 ms, RSS < 26 MB.
- Status: angenommen.

## D-06 Risiko-Ampel-Schwellwerte
- Status: offen (Phase 4).

## D-04 (aktualisiert 27.10.) Datenablage im `addon_config` des Job-Add-ons
- Angenommen. Job-Add-on schreibt nach `/config` (eigenes addon_config) mit `backup_exclude` für releases/raw;
  routing-core und valhalla mappen `all_addon_configs:ro`. Symlink-Umschaltung wie geplant.

## D-07 Auto/Rad für ganz DE offline auf dem Handy (Abweichung von der Spezifikation)
- Spezifikation: Auto/Rad auf dem Server mit Valhalla, nur Region. Vorgabe 27.10.: ganz DE, offline auf dem Handy
  „wie Organic Maps“.
- Folgen: Routing-Engine in der App (Kandidaten in Phase 7 vergleichen: Valhalla für Android, GraphHopper,
  BRouter-Datenformat), Kartenpakete pro Bundesland zum Nachladen (nur WLAN), Verkehrsmeldungen kommen weiter
  regionsweit vom Server und werden auf dem Handy auf die Route gelegt. Server-RAM wird dadurch eher entlastet.
- Offen: App-Größe/Akku messen; ob Valhalla auf dem Server dann nur noch für ÖPNV-Fußwege läuft.

## D-08 ÖPNV-Abdeckung: Region voll lokal + DE-Regionalzüge lokal, Rest über Transitous
- 27.10. Grund: ganz DE lokal sprengt RAM (Busse) und Job-Budget. `de_rv` (+ `de_fv`, nur aktiv bei
  ICE/IC-Schalter) zusätzlich ~30–60 MB mmap → in Phase 0 messen. NL/BE ausschließlich Transitous.
- Messung 27.10. (messungen/2026-10-27-raptor.md): StädteRegion + de_rv auf dem OptiPlex **+~20 MB RSS** (gesamt
  30–37 MB), Anfrage 0,9 ms Median, Laden 1,7 s / 90 MB Spitze. de_fv dazu: +~2 MB, Anfrage unverändert ~1,0 ms
  → der ICE/IC-Schalter kann ohne Speichersorge lokal bleiben.

## D-09 Echtzeit über offenen gtfs.de-Feed, nicht wie Öffi über AVV-HAFAS
- Messung 27.10. (messungen/2026-10-27-rt-abdeckung.md): ASEAG 93 % der laufenden Fahrten im Feed, 80 % mit Abweichung.
- Verworfen: Öffi-Weg (AVV-HAFAS `auskunft.avv.de/gate`, DB-Navigator-Schnittstelle) – inoffiziell, geheimer
  Schlüssel, Start/Ziel beim Betreiber. Entscheidung 27.10.: beim gewählten Weg bleiben.
- Status: angenommen. Stichprobe abends wiederholt (27.10. 21:40: ASEAG 89 % im Feed, 57 % mit Abweichung);
  Wochenende/Störung offen.

## D-10 OSM-Ausschnitt mit osmconvert statt osmium
- Messung 27.10.: `osmium extract` (smart, complete_ways) OOM bei 2 GB schon für eine Box aus dem 215-MB-Regbez.-
  Köln-PBF (ID-Mengen über den ganzen ID-Raum). `osmconvert --complete-ways --hash-memory=400`: 10 s, 0,8 GB RSS.
- Status: angenommen (technisch) für den Job `osm`; Debian-Paket `osmctools`, Image-Digest beim Bau festhalten.

## D-11 Valhalla 3.9.1 auf dem Server bauen (kein Laptop-Ausweichweg)
- Messung 27.10.: Regbez. Köln Tiles 49 s bei `concurrency=4`, Limit 2 GB nicht überschritten (Page Cache verdrängt).
  Dienst 200–216 MB RSS; Matrix 1×30 Median 49–58 ms, p95 ~100 ms → Ziel 50 ms knapp verfehlt, Tuning in Phase 1.
- Status: angenommen für den Bau; RAM des Dienstes und Matrix-Latenz offen (Phase 1).

## D-12 Fußwege zwischen Halten: Valhalla vorab, 4,3 km/h, höchstens 2 × Suchradius
- Messung 27.10. (messungen/2026-10-27-fusswege-valhalla.md): 14 000 Kandidatenpaare (Luftlinie ≤ 400 m) der
  StädteRegion in 7 s auf dem Server; echter Weg im Median 1,23× Luftlinie, Ausreißer bis 25× (Gleise, Autobahn).
- Entscheidung: Job `footpaths` rechnet die Umstiegswege vorab mit Valhalla (`sources_to_targets` je Quelle).
  Gehgeschwindigkeit **4,3 km/h** (1,2 m/s wie der bisherige Platzhalter) statt Valhalla-Standard 5,1 km/h,
  weil Umstiege mit Gepäck/Gedränge vorsichtig gerechnet werden sollen; Wege über **800 m** (2 × Suchradius)
  entfallen, mindestens 60 s Umstieg. Außerhalb der Valhalla-Tiles bleibt die Luftlinie.
- Gehtempo ist eine **App-Einstellung**, Standard „gemütlich“ = 4,3 km/h (27.10.). Umsetzung: Fußwege und
  Zugang werden einmal mit 4,3 km/h gerechnet; der Kern skaliert die Gehzeiten je Anfrage mit 4,3 / Tempo
  (Mindestumstieg 60 s bleibt), damit kein zweiter Fußweg-Datensatz nötig ist. **Umgesetzt** in routing-core
  0.1.8: Anfragefeld `"kmh"` (2–8, sonst 400), skaliert Zugangswege und Umstiege (`Router.WalkScale`).
- Erste/letzte Meile (27.10., routing-core 0.1.1): Kandidaten wie bisher (Luftlinie ≤ `access`, je Muster der
  nächste Halt), dann eine Valhalla-Matrix je Seite (Start und Ziel gleichzeitig, 4,3 km/h, Zeitgrenze 300 ms).
  Echte Wege über 2 × `access` entfallen. Fehler, Zeitüberschreitung oder Ort außerhalb der Tiles → Luftlinie.
- Status: **angenommen (27.10.).**

## D-13 Zugang über eine eigene Subdomain: NPM mit Client-Zertifikat, NPM-Fehlerprotokoll bleibt an
- Umgesetzt 27.10.: NPM-Proxy-Host `route.<domain>` → `<server>:8090` (routing-core), Let's Encrypt,
  Force SSL, HSTS, HTTP/2; Advanced: `ssl_client_certificate /config/custom_ssl/rwaysth-client-ca.pem`,
  `ssl_verify_client on`, `access_log off`. Test: ohne/fremdes Zertifikat 400, mit Handy-Zertifikat durchgelassen.
- CA + Client-Zertifikate auf dem Laptop (Sicherung im Passwortmanager), nie im Repo.
- **Abweichung (bewusst so entschieden, 27.10.):** Das NPM-Fehlerprotokoll (`proxy-host-<n>_error.log`,
  Stufe warn) schreibt bei Upstream-Fehlern Client-IP und Anfragezeile. Das bleibt so. Trotzdem: Koordinaten und
  Ziele gehen im Request-Body, nie in URL/Query, damit auch dort nur Pfade stehen. Für routing-core selbst gilt
  weiter: keine Anfragen, Ziele, Standorte in Logs.
- Status: angenommen.

## D-14 Job-Add-on `routing-jobs` und Valhalla-Add-on (Phase 1)
- `routing-jobs` (Go, `server/cmd/routing-jobs`) läuft im Valhalla-3.9.1-Image + `osmctools`, weil der Tile-Bau
  die Valhalla-Programme braucht. Jobs bisher `osm` und `valhalla`; `gtfs` und `footpaths` folgen.
- OSM-Quellen (Geofabrik): Regbez. Köln + Düsseldorf (Rhein-Kreis Neuss), Rheinland-Pfalz (Kreis Ahrweiler),
  NL-Limburg, Belgien (Lüttich, Ostbelgien – Geofabrik hat keine Provinzen). Zusammen ~1,3 GB Download; jede Quelle
  wird einzeln mit `osmconvert -B=region.poly --complete-ways --hash-memory=400` geschnitten, dann zusammengeführt.
  Prüfung: ≥ 1 Mio. Knoten und höchstens −10 % gegenüber dem vorigen Stand.
- Häufigkeit: Geofabrik ändert sich täglich, ETag hilft also kaum. Vorschlag: OSM + Valhalla **wöchentlich**
  (Automation, z. B. sonntags 04:00), GTFS täglich. Fußwege für die App ändern sich selten genug.
- Valhalla im Betrieb: nur `tiles.tar` (mmap), Cache 32 MB/Thread, 2 Threads, keine Zeitzonen/Verkehr, Protokoll
  nach `/dev/null` (Datenschutz). Kein Host-Port; routing-core spricht es im internen Netz an.
- Speicher: Budget = min(Option ≤ 2 GB, MemAvailable − 1 GB), gemessen wird `anon` der Container-cgroup (ohne
  Page Cache, vgl. Spike: `memory.peak` enthielt Page Cache). Überschreitung → Prozessgruppe beenden, Stand verwerfen.
- **Korrektur zu D-11 (Messung 27.10. abends, messungen/2026-10-27-jobs.md):** Mit Original-OSM-IDs braucht der
  Valhalla-Bau 2,1 GB (admins) bzw. **2,9 GB** anonym (tiles) – Bitsets über den ganzen ID-Raum, wie bei osmium.
  Der Spike lag nur scheinbar unter 2 GB, weil der Container in den Swap ausweichen konnte. Lösung: Der osm-Job
  nummeriert die IDs mit `osmium renumber` (kleine Warteschlangen per `OSMIUM_*`) neu: renumber 1,4 GB, admins
  0,6 GB, tiles 1,2 GB. OSM-IDs gehen dabei verloren; für Routing egal.
- Status: umgesetzt (0.1.1), Messung der ganzen Kette im Add-on folgt.
- **Job `gtfs` + täglicher Lauf (27.10. abends, routing-jobs 0.1.4, routing-core 0.1.6):** lädt `de_nv`/`de_rv`/`de_fv`
  von gtfs.de (ETag), schneidet `de_nv` wie bisher auf die StädteRegion-Box `5.95,50.48,6.45,50.93` (Option
  `gtfs_box`; Go-Port von `gtfs_bbox.py`, gleiches Ergebnis: 15 388 Fahrten, 7 119 Halte) und prüft mit dem
  RAPTOR-Lader für heute und in 6 Tagen (≥ 100 Muster, Halte höchstens −10 %). Danach Neustart von routing-core,
  das per Option `gtfs_dir` aus `gtfs/current` des Job-Add-ons liest. Laptop: 1,5 min, RSS 106 MB (GOMEMLIMIT 100 MiB).
- Zeitplan: **eine** Automation täglich 04:00 (Spieleserver leer, sonst +30 min bis 07:00) mit `gtfs,osm,valhalla,footpaths`.
  `osm` baut nur, wenn der Stand älter als `osm_tage` (7, minus 12 h Spielraum) ist; `valhalla` folgt dem OSM-Stand.
  So bleibt es bei einem Startweg und OSM + Valhalla werden trotzdem nur wöchentlich gebaut.
- **Job `footpaths` (routing-jobs 0.1.5, routing-core 0.1.7):** nach `gtfs`/`valhalla`, rechnet für alle Halte in
  `gtfs_box` die Wege zu Nachbarn ≤ 400 m Luftlinie mit einem kurz gestarteten `valhalla_service` (2 Threads) auf
  `valhalla/current/tiles.tar` (`sources_to_targets`, 4,3 km/h, wie footpaths.py) → `footpaths/current/footpaths.csv`.
  Neu gerechnet nur bei neuem GTFS- oder Tile-Stand. Prüfung: ≥ 1000 Paare, ≥ 80 % mit Weg, höchstens −10 % Wege.
  Lokal mit Valhalla-Ersatz: 3 354 Quellen, 14 027 Paare (27.10.: ~14 000). routing-core liest per Glob
  `/addon_configs/*_routing_jobs/footpaths/current/footpaths.csv`; ohne Treffer Luftlinie. routing-core wird nur
  einmal am Ende neu gestartet, wenn `gtfs` oder `footpaths` einen neuen Stand hatte.

## D-15 Echtzeit im Kern: Overlay auf dem Fahrplan, Abruf nur bei Anfragen (Phase 1, 27.10.)
- routing-core holt `realtime-free.pb` (gtfs.de, CC BY-SA 4.0) erst, wenn eine Anfrage kommt (D-05): höchstens alle
  30 s, `If-None-Match`, 6 s Zeitbudget je Versuch, ein Wiederholungsversuch, ein Abruf für alle gleichzeitigen
  Anfragen; Stände älter als 5 min gelten nicht. Behalten werden nur Fahrten des geladenen Fahrplans (abends
  27.10.: 5 847 von 39 000), Zuordnung über trip_id + start_date und stop_id, Verspätung gilt bis zur nächsten
  Angabe weiter (GTFS-RT-Regel). Laptop: erster Abruf 0,66 s, danach 18 ms.
- Gerechnet wird weiter auf dem **Fahrplan**; die Antwort trägt je Fahrt `dep_rt`/`arr_rt`, `canceled`, `skipped`
  und je Weg `arr_rt` + `risk` (`anschluss`: prognostizierte Ankunft + Fußweg nach der prognostizierten Abfahrt
  des Anschlusses; `ausfall`). Grund: RAPTOR mit verschobenen Zeiten bräuchte je Anfrage eine Kopie der
  Zeitachsen oder verletzt die Sortierung der Fahrten; die Ampel (D-06, Phase 4) baut auf `risk` auf.
- Neue Endpunkte: `POST /v1/departures` (Halte-IDs → nächste Abfahrten, Server sieht keinen Standort),
  `GET /v1/stops` (alle Halte für die App-Datenbank, ETag = Fahrplanstand). gtfs.de führt kein `trip_headsign`,
  als Fahrtziel gilt der Endhalt.
- 0.1.9 auf dem Server: Echtzeit-Abruf scheiterte („unknown authority“) – das leere Image hatte keine
  CA-Zertifikate; ab 0.1.10 aus dem Build-Image kopiert.
- Status: umgesetzt (routing-core 0.1.9, lauffähig ab 0.1.10).

## D-16 Adresssuche aus OSM im Kern (Phase 1, 27.10.)
- Job `geocode` liest den OSM-Stand mit `paulmach/osm` in einem Durchlauf (Knoten der Box sortiert im Speicher,
  Wege → Mittelpunkt, Multipolygone → Mittel der Wege) und schreibt Adressen, Straßen (je Name und ~1-km-Zelle),
  Orte und benannte Einrichtungen in `gtfs_box` als TSV. Laptop (Regbez. Köln): 13 s, 207 000 Einträge
  (180 000 Adressen), 12 MB.
- routing-core hält sie mit sortierter Wortliste + Trefferlisten (Wortanfänge, Umlaute gefaltet, „Str.“ → strasse,
  kurze Zahlen nur als Hausnummer) im RAM: Heap +38 MB, Suche 0,1–3 ms auf dem Laptop (Ziel 30 ms).
  `POST /v1/search` (Text im Body, nie in der URL oder im Protokoll); Wertung ohne Standort, die App sortiert
  nach Entfernung. RWTH-Gebäude kommen über ihre OSM-Namen (SuperC, Audimax …), RWTH Maps bleibt verworfen.
- Server 27.10. (Box mit NL/BE-Rand): 345 642 Einträge, Laden 1,7 s, Job 20 s / 235 MB. „vaals“ brauchte 63 ms,
  weil der Ort im Detail Tausender Adressen steht → 0.1.10: Kandidaten zuerst nur aus Namenswörtern (Detail wird
  nur geprüft), sonst alle mit Obergrenze 5000; Normalize in einem Durchlauf. Laptop danach: üblich < 1 ms,
  schlimmster Fall („52062“) ~10 ms.
- Status: umgesetzt (routing-jobs 0.1.7, routing-core 0.1.9).

## D-17 Offline-Karte: Ausschnitt der Protomaps-Basiskarte (Phase 1, 27.10.)
- Job `map` sucht den neuesten Tages-Build auf build.protomaps.com und holt mit `pmtiles extract` (go-pmtiles
  1.31.2, Prüfsumme im Image) per HTTP-Range nur die Kacheln der Box bis Zoom 15 → `map/current/region.pmtiles`,
  höchstens alle `osm_tage`. Lizenz beim Betreiber geprüft (docs.protomaps.com/basemaps/downloads, 27.10.):
  ODbL Produced Work, OSM-Namensnennung, keine Registrierung; direktes Verlinken unerwünscht, eigene Kopien
  empfohlen – genau das tut der Job. Kein eigenes Rendern (Planetiler/tilemaker bräuchten GBs RAM).
- routing-core liefert die Datei unter `GET /v1/map` (Range, Last-Modified); die App lädt sie einmal und danach
  nur bei Änderung. Die Karte kommt so nie von Dritten, der Standort verlässt das Handy nicht.
- Server 27.10.: Build 20261027, Box StädteRegion bis Zoom 15 → **49 MB**, 25 s, Spitze 109 MB; `pmtiles verify` ok.
- Status: umgesetzt (routing-jobs 0.1.7, routing-core 0.1.9).

## D-18 RWTH-Gebäudenummern über die interne Schnittstelle von RWTH Maps (Abweichung, 27.10.)
- **Abweichung** von „nur offene Quellen, keine inoffiziellen Schnittstellen“ (CLAUDE.md), vom Betreiber
  ausdrücklich so gewollt: Gebäudenummern (z. B. 1010, 2350) sind in Aachen das übliche Ziel, OSM hat sie nicht
  (keine `ref` mit Gebäudenummer), RWTH Maps nennt weder Lizenz noch offene Schnittstelle.
- Schnittstelle: `maps.rwth-aachen.de/navigator/libraries/rwth/ajax/getBuildings.asp?lang=de&q=…` (Suche nach
  Text oder Nummer) bzw. `&mode=type&q=b` (alle Gebäude, ~360, 245 KB). JSON in ISO-8859-1, kein Schlüssel.
- **App fragt live** (Betreiber: „live aus der App“): Der Suchtext geht dabei direkt an die RWTH (IP, Suchtext,
  kein Standort), parallel zu `/v1/search`. Bewusst in Kauf genommen; nur Text, nie Koordinaten.
- **Rückfall auf dem Server:** Job `rwth` holt bei jedem Lauf die ganze Liste → `rwth/current/gebaeude.tsv`
  (Art `gebaeude`, Name mit `[Nummer]`, Adresse als Detail), Prüfung ≥ 200 Gebäude und kein Einbruch > 10 %,
  neue Version nur bei Änderung (SHA-256). routing-core lädt sie mit der OSM-Suchliste in einen Index
  (`-geocode` mit Komma), `/v1/search` findet Gebäude nach Nummer und Name. Fällt RWTH Maps aus oder ändert sich
  die Schnittstelle, bleibt der letzte Stand; die App zeigt dann die Server-Treffer.
- Lokal 27.10.: 359 Gebäude mit Koordinate, 2 s; Live-Suche ~0,4 s.
- Status: umgesetzt (routing-jobs 0.1.8, routing-core 0.1.11, App).

## D-19 Wege auf der Karte: Fußwegform von Valhalla, Fahrten Halt für Halt, reiner Fußweg (28.10.)
- `/v1/route` liefert je Fußabschnitt (Zugang, Umstieg, Abgang) `shape` (encoded polyline, Genauigkeit 6, von
  Valhalla `/route`, pedestrian) und je Fahrt `stops` (Halte-IDs vom Ein- bis zum Ausstieg). Formen nur für die
  ausgegebenen Verbindungen, gleiche Wege einmal, alle gleichzeitig; ohne Valhalla oder bei Fehler fehlt `shape`
  und die App zeichnet gerade.
- Linienverlauf der Fahrten: keiner der Feeds hat `shapes.txt`; der Betreiber braucht ihn nicht genau → die App
  verbindet die Halte (Koordinaten aus ihrer SQLite) und markiert Zwischenhalte als kleine Punkte.
- **Reiner Fußweg** als eigene Verbindung (`trips: 0`, hinten angehängt): eine Valhalla-Route Start → Ziel,
  parallel zu den Zugangsmatrizen, nur bis 3 km Luftlinie (`-direct`) und höchstens 60 min beim gewählten Tempo
  (`-direct_max`). Ohne Halt in Reichweite, aber zu Fuß erreichbar, kommt nur der Fußweg statt Fehler 422.
- Status: umgesetzt (routing-core 0.1.12, App).
