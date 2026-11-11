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

## D-20 Karte für ganz Deutschland: Übersicht + Bundesländer zum Nachladen (28.10.)
- Betreiber 28.10.: „NRW komplett, Rest von DE grob und downloadbar bei Reinzoomen, so wie Organic Maps“.
- Job `map` holt einmal je `map_tage` (Standard 30) Deutschland samt Grenzgebiet (`map_box` 5.8,47.2,15.1,55.1)
  bis Zoom 15 aus dem neuesten Protomaps-Build (~8 GB Übertragung, D-17-Lizenz unverändert) und schneidet daraus
  lokal ohne Netz: `uebersicht.pmtiles` (ganze Box bis `map_uebersicht_zoom` 9) und je Bundesland eine Datei bis
  Zoom 15 (Rechtecke, Stadtstaaten beim Nachbarn: 13 Regionen, Liste in `maptiles.go`). Die Gesamtdatei wird danach
  gelöscht; `regionen.json` beschreibt den Stand. Platzbedarf mit Rollback ~25 GB, Prüfung auf 40 GB frei.
- Dry-Run 28.10. (Build 20261027): Übersicht Zoom 9 = 64 MB (Zoom 10 wäre 176 MB), NRW Zoom 15 = 1,4 GB,
  Gesamtbox Zoom 15 = 7,8 GB.
- routing-core: `GET /v1/map` → Liste `{regions:[{id,name,box,zoom,bytes,stand}]}`, `GET /v1/map/{id}` → Datei
  (Range, Last-Modified). Option `map` zeigt jetzt auf den Ordner `map/current`.
- App: lädt Übersicht und NRW selbst, weitere Regionen auf Wunsch (Hinweis beim Hineinzoomen in eine fehlende
  Region). Zwei Kartenebenen: Übersicht unten (wird über Zoom 9 hinaus vergrößert), Regionen darüber; fehlt eine
  Kachel in allen Regionen, scheint die Übersicht durch. Kein Kachelabruf übers Netz → der Server erfährt nur,
  welches Bundesland geladen wird, nie den Kartenausschnitt.
- Beschriftungen: Die Protomaps-Stile bauen Namen mit `format`, das der Renderer nicht kennt (→ keine
  Beschriftung). Die App ersetzt `text-field` durch deutschen Namen, sonst Namen vor Ort.
- Erster Lauf 28.10. (routing-jobs 0.1.10, Build 20261027): laden 4 min, schneiden 1 min, Spitze 701 MB;
  14 Dateien 9,3 GB (Übersicht 55 MB, NRW 1,39 GB, Bayern 1,83 GB, Saarland 83 MB).
- Status: umgesetzt (routing-jobs 0.1.10, routing-core 0.1.13, App).

## D-21 Fortbewegungsarten Einrad und Fahrrad (28.10.)
- Betreiber 27./28.10.: Einrad und Fahrrad wie Fußweg (gleiche Wege, Treppen ok), nur anderes Tempo; Einrad fährt
  meist gratis in Bus und Bahn mit; Fahrrad nie im Bus, Bahn nur wenn nötig → Hinweis „Fahrradticket nötig“.
  Tempo je Art kalibrierbar per GPS-Messgang in der App (bleibt auf dem Handy).
- `POST /v1/route` nimmt `"art": "fuss" | "einrad" | "rad"` (Standard fuss) mit `kmh`; Grenzen fuss 2–8,
  einrad 4–20, rad 6–35 km/h. Zugangsradius und Grenze des direkten Wegs wachsen mit kmh/4,3 (höchstens ×4).
- Rad: nur Schiene (GTFS-route_type Zug/S-/U-/Straßenbahn, `IsBahn`), Zugang nur zu Halten mit Schienenverkehr;
  Fahrten tragen `"rad": true`; der direkte Weg steht vorn. Valhalla rechnet weiter mit Fußgänger-Kosten
  (Annahme: Wegewahl wie zu Fuß, siehe oben); eigene Fahrrad-Kosten erst mit Fahrradnavigation.
- Status: umgesetzt (routing-core 0.1.14); App-Profile folgen.
- 28.10. (Betreiber): Einrad heißt in der App „Custom“ (zwei selbst benannte Profile), Tempo frei: Server nimmt für
  `einrad` jedes `kmh` > 0 an (0 und negativ ergäben keine Wegzeit), routing-core 0.1.28.

## D-22 Fahrradtypen: Klapprad, Fixie, Rennrad, Mountainbike (28.10.)
- Betreiber 28.10.: Fußwege sind nur mit dem Rennrad doof. Klapprad kann in Bus und Bahn mit; Fixie leicht, kann
  Treppen getragen werden, fährt fast so schnell wie das Rennrad; Rennrad schnell, Treppen vermeiden (geht aber in
  Ordnung); Mountainbike nicht so schnell, kann überall lang, Treppen hoch vermeiden. Je Typ 2–3 Tempostufen.
- `art`: `klapprad` (6–30 km/h, alle Linien, kein Fahrradticket), `fixie` (6–40, nur Schiene + Fahrradticket;
  `rad` aus D-21 gilt als Fixie), `rennrad` (10–45, Schiene + Ticket), `mtb` (6–35, Schiene + Ticket).
  Bei allen Rädern steht der direkte Weg vorn.
- Wege: Klapprad und Fixie wie zu Fuß (Valhalla `pedestrian`, Zeit skaliert). Mountainbike ebenso, aber
  `step_penalty` 180 s – Valhalla unterscheidet bei Treppen nicht nach Richtung, also werden Treppen allgemein
  gemieden, nicht nur aufwärts. Rennrad: Valhalla `bicycle` (`bicycle_type` Road, `cycling_speed` = Tempo,
  `use_roads` 0,6, `avoid_bad_surfaces` 0,8) → keine Fußwege ohne Radfreigabe, keine Treppen, Zeit direkt von
  Valhalla; findet das keinen Weg, Rückfall auf Fußwege mit `step_penalty` 600 s (Treppe tragen „geht in Ordnung“).
- Umstiege zwischen Halten (Fußwege-Tabelle) bleiben für alle Arten Fußwege mit skaliertem Tempo.
- Zugangsradius: mit allen Linien (Fuß, Einrad, Klapprad) höchstens ×2,5 statt ×4 – das Klapprad mit ×4 brachte so
  viele Bushalte in die Valhalla-Matrix, dass sie 111 ms brauchte (Grenze 100). Steht der direkte Weg vorn, entfallen
  RAPTOR-Wege ohne Fahrt (sonst doppelt).
- Klapprad meidet Busse leicht (Betreiber 28.10.: „kann minimal schneller sein als ein Bus bei ein paar
  Stationen“): Verbindungen mit Bus entfallen, wenn sie nicht mindestens 5 min vor dem direkten Weg ankommen
  (`BusMalus`); Bahnen bleiben ohne Malus.
- Profile und Tempo bleiben auf dem Handy; der Server bekommt je Anfrage nur `art` + `kmh` (Betreiber 28.10.: „reicht,
  wenn die Geschwindigkeit vom Handy vorgegeben wird … dann lässt sich die App auch teilen“).
- Status: umgesetzt (routing-core 0.1.18: Bus-Malus 5 min statt 3, Betreiber 28.10.; App).

## D-23 Ausstiegs-Erinnerung per GPS auf dem Handy (28.10.)
- Glocke in der Detailansicht startet einen Android-Vordergrunddienst (`AusstiegDienst.kt`, Typ `location`), der die
  Halte aller Fahrten der Verbindung (Koordinaten aus der lokalen SQLite) und die Ankunftszeit (Echtzeit, sonst
  Fahrplan) bekommt. GPS/Netz-Standort alle 4 s: Halt gilt als erreicht unter 120 m (+ halbe Ungenauigkeit), nur
  vorwärts, höchstens 3 Halte weit – außer kurz vor der Ankunft (eine Linie kann spätere Halte früh streifen).
- Vibration + Meldung „Gleich aussteigen“ beim Halt vor dem Ausstieg oder < 700 m vor dem Ziel-Halt; ohne GPS
  (Tunnel) 2 min vor der Ankunftszeit. Danach weiter mit der nächsten Fahrt („Umsteigen: Linie …“); Ende 1 min nach
  dem letzten Ausstieg, spätestens 30 min nach der geplanten Ankunft. Laufende Meldung „noch N Halte“ mit „Beenden“.
- Datenschutz: Positionen bleiben im Dienst (nur RAM), nichts geht an den Server.
- Status: umgesetzt (App), Praxistest offen.

## D-24 Fahrplan für ganz NRW (28.10.)
- Betreiber 28.10.: „ich komme nicht mal nach Bergneustadt, also muss noch mehr rein“; langsamere Antworten für
  selten genutzte Gebiete sind in Ordnung (SSD im Server).
- `gtfs_box` 5,85,50,3,9,5,52,55 (NRW samt Rand) statt Raum Aachen: 450 658 Fahrten, 199 420 Halte, 37 806 Linien
  (vorher 24 707 Halte). Laptop-Messung: Zuschnitt 51 s, Spitze 528 MB; routing-core 258 MB im Betrieb
  (417 MB Spitze beim Laden), RAPTOR Aachen–Bergneustadt 40–45 ms, Köln–Düsseldorf 21 ms, in Aachen ~2 ms.
- **Abweichung:** RAM-Ziel 250 MB für alle neuen Add-ons wird überschritten (Core ~260 + Valhalla ~90 MB), die harte
  Grenze 500 MB hält. GOMEMLIMIT routing-core 200 → 400 MiB.
- Fußwege (Valhalla) gibt es weiter nur in den OSM-Quellen (RB Köln, RB Düsseldorf, RLP, Limburg, Belgien);
  in RB Münster, Arnsberg, Detmold gilt die Luftlinie, bis die OSM-Quellen wachsen.
- Plan für ganz Deutschland (Vorschlag Betreiber): zweistufig – erst über ein Bahn-Netz (DE-weit, nur Schiene)
  in die Nähe des Ziels (z. B. Gummersbach für Bergneustadt), dann im Detail im Nahverkehr der Zielregion; hält
  Speicher und Suchraum klein. Ausweichweg bei knappem RAM: Fahrplandaten per mmap von der SSD.
- OptiPlex 28.10.: RAPTOR Aachen–Bergneustadt 46–69 ms (erste Anfrage nach dem Start 87–99 ms), Köln–Düsseldorf
  31 ms, in Aachen 2–5 ms. Footpaths-Job 10 min (372 308 Paare, 123 593 außerhalb der Tiles, Spitze 538 MB);
  `/v1/stops` mit gzip 2,2 MB statt 6,4 MB.
- Status: umgesetzt (routing-core 0.1.20, routing-jobs 0.1.11, Job-Option gtfs_box).

## D-25 Kein Halt in Gehweite: Suche erweitern statt Fehler (28.10.)
- Betreiber 28.10.: „eigentlich müsste jeder Flecken Land in Deutschland irgendwie erreichbar sein“.
- Bleibt auf einer Seite im Suchradius (800 m × Reichweite) kein Halt übrig (auch wenn Valhalla alle echten Wege
  verwirft), sucht routing-core diese Seite mit doppeltem Radius weiter bis `-far` (Standard 15 km Luftlinie ×
  Reichweite). In der erweiterten Suche zählen nur die nächsten Halte: höchstens 12, bis 15 min über dem nächsten;
  echte Wege bis 2× Radius, Valhalla-Zeitlimit wächst mit (höchstens 8×). Der Normalfall bleibt unverändert.
- Findet sich auch bis 15 km nichts, liegt der Ort außerhalb des Fahrplangebiets (bisher NRW, D-24). Die Antwort
  bleibt `keine_haltestelle_in_reichweite` (alte Apps), neu mit `seite` (start | ziel | beide); die App nennt die Seite.
- Betreiber 28.10.: „bei mehr als 5km fußweg soll ne grobe zeitschätzung angezeigt werden und ob die route wirklich
  im detail berechnet werden soll“. Liegt der nächste Halt einer Seite weiter als `-lang` (5 km Luftlinie ×
  Reichweite), rechnet der Server weder Valhalla noch RAPTOR, sondern antwortet 422 `fussweg_lang` mit `start`/`ziel`
  = {km, min} (Luftlinie × 1,3, auf 0,5 km / 5 min gerundet). Die App zeigt die Schätzung und „Trotzdem genau
  berechnen“ → dieselbe Anfrage mit `"lang": true`. Ist Start→Ziel direkt zu Fuß möglich, kommt der Fußweg.
  Alte Apps zeigen dann „Route nicht berechnet (fussweg_lang)“ (vorher kam dort ohnehin kein Ergebnis).
- Ganz Deutschland erreichbar wird es erst mit dem DE-Fahrplan (zweistufiger Plan in D-24).
- Status: umgesetzt (routing-core 0.1.24; 0.1.23 ohne Nachfrage nicht ausgerollt).

## D-26 Adresssuche für ganz Deutschland: eigene Länderquellen, Index-Datei per mmap (28.10.)
- Betreiber 28.10.: Adresssuche überall (ganz DE); Daten dürfen auf der SSD liegen, RAM im Betrieb < 1 GB.
- Job `geocode` liest nicht mehr den Regionsausschnitt des osm-Jobs, sondern ganze Geofabrik-Gebiete (neue Option
  `geocode_sources`, Standard europe/germany, netherlands/limburg, belgium; md5 + ETag wie beim osm-Job, höchstens
  alle `osm_tage`). Keine Box mehr: die Suche reicht weiter als der Fahrplan (D-24).
- Extraktion in drei Durchläufen je Datei (osmpbf Skip*): Relationen → gesuchte Wege, Wege → je Eintrag ein Knoten
  (der mittlere; bei Gebäuden eine Ecke, ~20 m daneben – der zweite Knoten für die Mitte kostete ~300 MB), Knoten →
  nur deren Koordinaten. Einträge gehen sofort in Dateien je 0,5°-Zelle, werden dort sortiert und entdoppelt
  (auch Grenzgebiete in zwei Quellen) und nach Zellen geordnet zu geocode.tsv verkettet. Nächster Ort einer Straße
  über ein 0,25°-Gitter.
- geocode.idx (neues Format, Kopf `SWGIDX01`): Zeilenanfänge, Koordinaten (1e-6°), sortierte Wörter,
  Trefferlisten (Eintrag<<1 | Wort im Namen), Zellentabelle. routing-core blendet TSV + Index per mmap ein; im Heap
  liegt nichts Dauerhaftes (vorher ~130 MB für 1 Mio. Einträge). Kleine Listen ohne .idx (RWTH-Gebäude) baut es beim
  Start im Speicher.
- Suche: Trefferliste des seltensten Suchworts, geschnitten mit den weiteren, bis ≤ 5000 Kandidaten (sonst zuerst
  die mit dem Wort im Namen); Wörter mit > 2 Mio. Treffern („5“ samt Postleitzahlen 5xxxx) nur prüfen. Mit `near`
  (App schickt den Standort auf 0,05° gerundet, nur für die Wertung, nie protokolliert) zusätzlich ein Durchgang über
  die 3 × 3 Zellen um den Nutzer, Bonus < 3 / 15 / 60 km, bei Gleichstand das Nähere. Gleicher Name + Detail
  < 200 m nur einmal. Alte Apps ohne `near` bekommen die Suche ohne Nähe.
- **Reihenfolge beim Ausrollen:** routing-core bis 0.1.24 lehnt `near` ab (unbekanntes Feld → 400), also erst den
  Server, dann eine App ab 219d12f.
- Laptop 28.10. (DE + NL-Limburg, GOMEMLIMIT 1,5 GiB wie im Job): 20,9 Mio. Adressen, 1,6 Mio. Straßen, 102 000 Orte,
  2,2 Mio. Einrichtungen; Extraktion 6:54 min (anonymer Speicher bis 1,09 GB), Index 1:46 min (bis 1,54 GB, darin
  ~0,5 GB Trefferlisten); TSV 1,44 GB, Index 0,73 GB.
- OptiPlex 28.10. (germany + NL-Limburg + Belgien): 24,96 Mio. Adressen, 1,85 Mio. Straßen, 110 000 Orte, 2,46 Mio.
  Einrichtungen; Job 10:30 min (Liste 4:20, Index 0:49, Rest Download), Spitze 1,49 GB von 2 GB. Suche mit `near`
  1–15 ms (erste Anfrage nach dem Laden bis 46 ms, Seiten kommen von der SSD); routing-core danach 172 MB anonym
  (vorher 346 MB). Geofabrik leitet germany-latest auf einen Spiegel um, die .md5 liegt nur dort (routing-jobs 0.1.13).
- Speicher des Jobs (28.10., Betreiber: < 1 GB): Der Index entsteht direkt in seiner eingeblendeten Datei (erst
  zählen, dann Zeilen, Koordinaten und Trefferlisten an ihren Platz schreiben), im Heap steht nur die Wortzählung.
  Knoten-IDs mit 4 statt 8 Byte (untere 32 Bit, Grenzen je oberem Teil), gesammelt in Blöcken statt Verdoppeln;
  Zellen werden über Zeilennummern sortiert statt über Entry-Strings. Go-Grenze im Job höchstens 800 MiB statt
  ¾ von max_mem_mb (der Heap wuchs sonst bis 1,5 GiB, lebendig ist weit weniger). NRW auf dem Laptop:
  Index-Spitze 517 → 72 MB (gleiche Datei, byte-gleich), Extraktion 376 → 362 MB und 1:22 → 1:13 min; lebendig
  in der Extraktion ~90 MB PBF-Puffer (wachsen nicht mit dem Gebiet) plus eigene Listen.
- RWTH-Gebäude bekommen ihren Vorrang (+3) nur, wenn ein Suchwort im Namen oder der Nummer steht, nicht bei
  „aachen“ oder „52062“ (vorher kamen dort fast nur Gebäude).
- Status: umgesetzt (routing-core 0.1.25, routing-jobs 0.1.13); Speicher und Gebäude-Wertung in routing-core 0.1.27,
  routing-jobs 0.1.14.

## D-27 Planung nach Ankunftszeit (28.10.)
- Betreiber 28.10.: „ich würde gerne noch ankunfts-/abfahrtszeit planen können“.
- POST /v1/route: neues optionales Feld `ankunft` (bool); `time` ist dann die späteste Ankunft. RAPTOR rechnet weiter
  nur vorwärts: erst ab 1 h vor der Ankunft (bei Bedarf 2, 4, 8 … h zurück bis Betriebstagbeginn), dann wiederholt
  eine Minute nach der spätesten passenden Abfahrt, bis nichts Späteres mehr ankommt. Je Umstiegszahl bleibt die
  späteste Abfahrt; mehr Umstiege nur, wenn man damit später losgehen kann. Reiner Fußweg endet genau zur Ankunft.
- **Abweichung:** eine Anfrage sind so typisch 3–6 RAPTOR-Läufe (Grenze 100 ms gilt je Lauf, nicht je Anfrage);
  ein Rückwärts-RAPTOR wäre schneller, lohnt aber erst bei Bedarf.
- App: Chip unter „Nach“ (Jetzt losgehen / Abfahrt hh:mm / Ankunft hh:mm), nur für heute, weil nur der heutige
  Betriebstag geladen ist. Alte Apps senden das Feld nicht.
- Status: umgesetzt (routing-core 0.1.25, zusammen mit D-26 ausgerollt; 0.1.26 entfiel).

## D-28 Eigener Signierschlüssel, APK aus der CI (28.10.)
- Betreiber 28.10.: jetzt umstellen (vorher Debug-Schlüssel). Wer die alte APK hat, muss einmal deinstallieren
  (Favoriten, Einstellungen, Offline-Karten gehen dabei verloren).
- RSA 4096, PKCS12, Alias `schnellweg`, CN=SchnellWeg, SHA-256 des Zertifikats
  10:63:32:fa:1c:dd:a1:7a:6c:48:bc:64:f3:21:17:d0:af:21:75:25:c8:05:8d:6f:c3:f8:23:54:7c:ff:98:20.
  Keystore und Passwort liegen nur beim Betreiber (außerhalb des Repos, Sicherung im Passwortmanager);
  `android/key.properties` (gitignored) zeigt lokal darauf, die CI liest Secrets.
- `.github/workflows/app.yml`: Tag `app-v<version>` → Tests, signierte Release-APK, check-apk.sh, Fingerabdruck in
  der Zusammenfassung, APK als Workflow-Artefakt (30 Tage). Flutter per git-Tag (kein fremdes Action-Paket).
- Status: umgesetzt; CI braucht die Secrets SCHNELLWEG_KEYSTORE_B64 und SCHNELLWEG_KEYSTORE_PASSWORT.

## D-29 Losgeh-Wecker aus dem Android-Kalender (28.10.)
- Betreiber 28.10.: Termine aus dem Android-Kalender (nicht über HA/Server); Meldung vorerst ohne Ton und Vibration.
- Nächster Termin mit Ort heute in den nächsten 12 h (ganztägige zählen nicht; nur heute, weil der Server nur den
  heutigen Betriebstag kennt, D-27) → Ortstext über /v1/search (mit `near`, wie eine eingetippte Suche) →
  /v1/route mit `ankunft` = Beginn − Puffer → späteste passende Abfahrt → AlarmManager zur Losgehzeit − Vorlauf
  (exakt, wenn erlaubt; sonst ±1 min Fenster). Termine, Titel und Zeiten verlassen das Handy nicht.
- Gerechnet wird nur bei offener App (Start/Rückkehr, höchstens alle 5 min, CLAUDE.md: kein Polling ohne offene App);
  der gestellte Wecker meldet sich auch bei geschlossener App, Echtzeit ist dann aber so alt wie die letzte Rechnung.
  Nachtrag 28.10.: 10 min vor der Meldung rechnet ein zweiter Wecker einmal im Hintergrund nach (Flutter-Engine ohne
  Oberfläche, gleicher Start/Ziel/Tempo wie bei der letzten Rechnung in losgehen.json, aktuelle Echtzeit); einmal je
  Plan, kein Polling. Fährt die Verbindung früher, kommt die Meldung sofort; ohne Netz bleibt der alte Wecker.
- Startseite: oberste Zeile der Haltestellenliste zeigt Termin + „Los hh:mm · Linie X ab Halt hh:mm · an hh:mm“,
  Antippen setzt ihn als Ziel mit Ankunftszeit. Einstellungen → Losgeh-Wecker: an/aus (fragt READ_CALENDAR),
  Vorlauf, Puffer (je 0–20 min, Vorgabe 5/5). Kanal „Losgeh-Wecker“ ohne Ton/Vibration (in Android umstellbar).
- Läuft neben dem HA-Kalender-Wecker (Aufwachen), ersetzt ihn nicht.
- Status: umgesetzt (App).

## D-30 Alle Räder mit Valhallas Fahrradkosten (28.10.)
- Bisher fuhr nur das Rennrad mit Fahrradkosten; Klapprad, Fixie und MTB gingen Fußwege mit skalierter Zeit
  (zu kurze Wege durch Fußgängerzonen und Parks, Zeiten aus dem Gehtempo hochgerechnet).
- Jetzt je Typ `bicycle_type`: Klapprad City, Fixie Hybrid, Rennrad Road, MTB Mountain; `avoid_bad_surfaces`
  0,5 / 0,5 / 0,8 / 0. Rückfall auf Fußwege mit Treppenstrafe (120 / 300 / 600 / 180 s), wenn es keinen Radweg gibt.
  Einrad bleibt bei Fußgänger-Kosten.
- Messung am Server (Markt → Hbf, 18 km/h): Fuß 1,30 km; Rad 1,59 km, 340 s (alle Typen gleich, Unterschiede erst
  bei Schotter/Straßentypen).
- Status: umgesetzt (routing-core 0.1.26).

## D-31 Mehrere Verbindungen und knappe Verbindungen (30.10.)
- Betreiber 30.10.: meist kam nur eine Verbindung (RAPTOR liefert die früheste Ankunft je Umstiegszahl, fast immer
  dieselbe); gewünscht mehrere Optionen, es zählt die schnellste. Wer 1–2 min zu spät dran ist, soll die Verbindung
  trotzdem sehen, wenn sie mit höherem Tempo noch klappt – mit Hinweis aufs Tempo.
- Ohne `ankunft`: nach der Abfrage ab `time` erneut ab frühester Losgehzeit + 1 min, bis 4 Verbindungen da sind
  (höchstens 5 Abfragen). Doppelte und dominierte (früher los, später an, nicht weniger Umstiege) fallen raus,
  sortiert nach Ankunft. Abweichung vom Zeitbudget: das Ziel < 50 ms gilt je RAPTOR-Abfrage, `ms` in der Antwort
  ist jetzt die Summe.
- `knapp: true` (neue App): zusätzlich eine Abfrage ab `time` − 150 s. Verbindungen, für die man vor `time` hätte
  losgehen müssen, kommen vorn mit `eil_kmh` (nötiges Tempo auf dem Zugangsweg, auf 0,5 aufgerundet), wenn sie
  früher ankommen als die schnellste normale, der Zugangsweg höchstens doppelt so schnell sein muss und das Tempo
  die Grenze nicht übersteigt (zu Fuß 12 km/h = Laufen, Räder ihr Höchsttempo, Custom ohne Grenze). Umstiege
  bleiben beim gewählten Tempo. Losgehzeit = `time`.
- App: schickt `knapp` (nur ohne Ankunftszeit), zeigt „⚡ Knapp: nur mit X km/h zum ersten Halt“, Karte wählt zuerst
  die schnellste normal erreichbare. Alte Apps bekommen nur die zusätzlichen Abfahrten (Feld `knapp` fehlt).
  Neue App braucht einen Server mit D-31 (unbekannte Felder → 400).
- Nachtrag 02.11.: Bei dichtem Zubringer (Elisenbrunnen→Düren bei 4,3 km/h, Bus alle 1–2 min zum Zug) brauchten
  spätere Busse zum selben Zug alle 5 Abfragen auf, am Ende schlug der letzte die übrigen: nur eine Verbindung.
  Jetzt zählen nur ungeschlagene Verbindungen, höchstens 10 Abfragen; ohne neue Ankunft verdoppelt sich der Schritt,
  mit neuer halbiert er sich (mindestens 1 min). Springt er über den letzten Bus, geht es dort neu los. Danach bis zu
  4 Abfragen, um zur schnellsten Verbindung den spätesten Bus zu finden. Verbindungen, die ≥ 30 min und > 1,5× länger
  dauern als die kürzeste, fallen raus (live: Bonn über Nachtbus 5 h 44 statt 2 h), außer früheste Ankunft und reiner Weg.
- Status: umgesetzt.

## D-32 Autorouten (02.11.)
- Betreiber 02.11.: die Routenberechnung fürs Auto fehlte noch. Neue Art `auto` in POST /v1/route: Valhalla-Kosten
  `auto`, eine Verbindung von Tür zu Tür ohne ÖV, unabhängig vom Fahrplan (auch `anderer_betriebstag` entfällt).
  Antwort: ein Abschnitt `mode: "drive"` mit Wegform. Alte Apps fragen `auto` nicht an und sehen nichts davon.
- `kmh` (30–200, seit D-35 30–500, App: Profil „Auto“, Vorgabe 130) ist das Höchsttempo (`top_speed`), kein Reisetempo; deshalb kein
  Messgang. Live Aachen Hbf → Köln Hbf: 130 km/h 52 min, 80 km/h 80 min, je ~78 km.
- `search_cutoff` 2 km statt Valhallas 35 km: sonst endete die Route für Ziele außerhalb der Tiles still am Rand.
  Kein Weg → 422 `kein_autoweg`, Valhalla nicht erreichbar → 503 `auto_aus`.
- Grenzen: nur im Gebiet der Tiles (`config/region.geojson`: Aachen, Köln, Bonn, Düren, Nordeifel, Süd-Limburg,
  Lüttich), nicht in ganz NRW wie der Fahrplan. Keine Verkehrslage und keine Parkplatzsuche in der Zeit.
- Zeitbudget: Abweichung vom Ziel für erste/letzte Meile (< 100 ms): Aachen–Köln gemessen ~360 ms, die Anfrage hat
  5 s statt 300 ms. Valhalla rechnet sie mit seinen vorhandenen Threads, keine Dauerlast.
- Status: umgesetzt.

## D-33 Ein Gehtempo mit Puffer (02.11.)
- Betreiber 02.11.: Für zu Fuß reicht eine Gehgeschwindigkeit; ein Puffer in beide Richtungen soll zeigen, ob man
  für die Verbindung entspannter oder schneller gehen muss.
- Statt der drei Fuß-Profile gibt es eines (Schlüssel bleibt `entspannt`, Anzeige „Zu Fuß“, Standard 4,3 km/h).
  Alte tempo.json: War `normal`/`schnell` aktiv, wird dessen Tempo das Gehtempo, sonst bleibt das von `entspannt`.
- Die App rechnet je Verbindung das nötige Tempo zum ersten Halt (Gehtempo × Gehzeit / Zeit bis zur Abfahrt, mit
  Echtzeit) und vergleicht mit dem Gehtempo ± 20 % (`tempoPuffer`): darunter „Entspannt“, im Puffer „Normales
  Tempo passt“, darüber „Schneller gehen“. Nur zu Fuß und ohne Ankunftszeit; knappe Verbindungen behalten den Hinweis
  aus D-31.
- Keine Server-Änderung, ältere Apps sind nicht betroffen.
- Status: umgesetzt.

## D-34 Profile in der App, Server bekommt nur „meiden“ (02.11.)
- Betreiber 02.11.: Alle Tempoeinstellungen gehören als Profile in die App, Fahrradprofile u. Ä. legt er selbst an;
  an den Server geht nur, was er meiden soll.
- `meiden` in POST /v1/route ersetzt die feste Art: Stufen 0–1 `treppen`, `belag`, `hauptstrassen`, `steigung`,
  `faehren`, `dunkel`, `autobahn`, `maut`; Schalter `fusswege` (Fahrradkosten mit Rückfall auf Fußwege), `bus` (nur
  Schiene), `oev` (nur direkter Weg, ohne Fahrplan und Längengrenze, wie beim Auto); `bus_min`, `umstiege`.
  Tempo mit `meiden` frei über 0. Das Auto behält `art: auto`, weil es sich nicht über Meiden beschreiben lässt.
- `art` bleibt für ältere Apps unverändert (Natalia), `art` außer `auto` zusammen mit `meiden` → 400.
- Fahrradticket ist nur noch ein Hinweis der App (`ticket` im Profil), Fernverkehr ein Schalter im Profil (`fern`).
- tempo.json bekommt `v: 2`. Ältere Stände werden einmalig umgestellt: alle festen Profile werden eigene mit ihren
  Tempi, Messungen, Namen und bisherigen Regeln; Neuinstallationen haben nur „Zu Fuß“ und „Auto“. Fahrradtyp folgt
  aus `belag` (≥ 0,8 Road, 0 Mountain, sonst Hybrid), das Klapprad rechnet damit Hybrid statt City.
- Neue App braucht einen Server mit D-34 (unbekanntes Feld → 400).
- Nachträge nach Prüfung: Valhalla ignoriert `cycling_speed` außerhalb 5–60 km/h → Randwert schicken, Zeit skalieren.
  Steigungen ohne Regler, weil die Tiles keine Höhendaten haben (`/height` liefert null); `steigung` bleibt im Server.
  Beim Auto schließt `belag` Feldwege nur aus, wenn es einen anderen Weg gibt. Knapp bei eigenen Profilen höchstens
  1,5 × Tempo, Weg ohne Fahrt nur einmal in der Liste. tempo.json wird atomar geschrieben, nicht lesbare Stände landen
  als `tempo.json.kaputt`.
- Status: umgesetzt.

## D-35 Auto: Tempo zählt auf Autobahn ohne Limit (02.11.)
- Betreiber 02.11.: Auto soll auch deutlich schneller als 200 gehen (Beispiel 350 km/h). Allein die Grenze anheben brächte
  nichts: Valhalla rechnet Autobahn mit `speed_limit: unlimited` pauschal mit 105 km/h, `top_speed` senkt nur. Aachen–Köln
  blieb von 130 bis 350 km/h bei 52 min (54 von 78 km sind solche Autobahn).
- Grenze jetzt 30–500 km/h. Nach der Route fragt der Server `/trace_attributes` (live ~8 ms) und zieht die gesparte Zeit ab:
  nur Kanten `motorway` + `unlimited`, dort gilt `kmh`. Angegebene Limits, Auffahrten und alle anderen Straßen bleiben wie
  von Valhalla; `fixed_speed` schied aus, es gälte auch in der Stadt (23,9 min Aachen–Köln bei 200).
- Ohne Kantendaten bleibt Valhallas Zeit. Die Route selbst bleibt Valhallas Wahl, sie kennt das höhere Tempo nicht.
  Beschleunigen, Bremsen und Verkehr sind nicht eingerechnet.
- Betreiber 02.11.: Vorgabe für neue Installationen 120 statt 130 km/h, als realistischer Schnitt auf freier Autobahn; höhere Werte
  bleiben wählbar. Baustellen zählen nur, soweit ihr Limit in OSM steht (Kartenstand, keine Live-Daten).
- Status: umgesetzt.

## D-36 Neu berechnen beim Abweichen (02.11.)
- Betreiber 02.11.: Weicht man von der Route ab, soll sofort neu berechnet werden. Die App hat keine Navigation im
  Hintergrund; das GPS läuft nur, solange die Startseite sichtbar ist und die gewählte Verbindung gerade läuft: ohne Fahrt bis
  30 m vor dem Ziel, mit Fahrt nur auf dem Fußweg bis zur Abfahrt (Fahrten sind von Halt zu Halt gerade, taugen nicht als
  Vergleich). Nichts wird gespeichert. Ausnahme von „Kein Dauer-Tracking“ in `standort.dart`, begründet durch den Wunsch.
- Abgewichen heißt: 3 Fixe in Folge mehr als 60 m (Auto) bzw. 40 m plus GPS-Genauigkeit neben dem Weg; Fixe ungenauer als 50 m zählen
  nicht. Neu höchstens alle 15 s. Die alte Route bleibt stehen, bis die neue da ist, und bleibt bei einem Fehler.
- Status: umgesetzt.

## D-37 /v1/departures antwortet nie mit null (02.11.)
- Fund: Halt ohne Abfahrt im Zeitfenster oder unbekannter Halt ergab `null`; die App brach daran ab (`l as List`, kein ApiFehler),
  die Liste „Haltestellen in der Nähe“ blieb leer, ohne Hinweis und ohne weiteren Abruf. Vorgesehen war `null` als Zeichen,
  /v1/stops neu zu laden, das hat die App nie umgesetzt.
- Server liefert jetzt immer `[]`, damit sind auch vorhandene Apps (Natalia) ohne Update sicher; die App verträgt beides.
- Status: umgesetzt.

## D-38 Verkehrslage bei Autorouten aus der Autobahn-API (02.11.)
- Betreiber 02.11.: Stau bei Autofahrten zeigen, Baustellen in der Zeit berücksichtigen; Quelle ausdrücklich die Autobahn-API
  (verkehr.autobahn.de). Grenzfall: beim Betreiber keine Lizenz gefunden, Doku nur von Dritten (bund.dev), private Nutzung. Mobilithek
  (offiziell, Registrierung) nicht gewählt. Bei einer Weitergabe der App neu prüfen.
- Datenschutz: Abruf immer für alle Autobahnen der Region (`verkehrStrassen`, Box), nie zur einzelnen Route; der Abgleich läuft auf dem
  Server. Kein Polling: nur bei Autorouten, Stand 3 min frisch, höchstens 30 min alt, nur im RAM. Eine Anfrage wartet höchstens 3 s,
  danach gilt der vorige Stand; Budget des Abrufs 8 s, 4 Anfragen gleichzeitig.
- Abgleich: mindestens die Hälfte einer Meldung liegt höchstens 60 m neben der Route und läuft in dieselbe Richtung (die Form der Meldung
  verläuft in Fahrtrichtung, so zählt die Gegenfahrbahn nicht). Von überlappenden Staus bleibt der mit der größten Verzögerung.
- Fahrzeit: Verzögerung der Staus kommt dazu; in Baustellen gilt `Max. N km/h` aus der Beschreibung, wenn es unter dem sonstigen Tempo liegt.
- Grenzen: nur Autobahnen. Die Route weicht Staus und Sperrungen nicht aus, die App zeigt sie nur.
- Status: umgesetzt.

## D-39 Umfahrung nur als Angebot, Abbiegehinweise (02.11.)
- Betreiber 02.11.: nicht lenken wie Google Maps, Waze oder Apple Karten, weil man dort oft im Stau landet, den die Umleitung selbst erzeugt;
  umfahren nur, wenn es Zeit spart. Die Umfahrung ist deshalb eine zweite Verbindung, die nie von selbst gewählt wird.
- Angeboten nur bei Stau ab 5 min, wenn sie mindestens 5 min und 10 % der Fahrzeit spart und höchstens 2 km mehr auf kleinen Straßen fährt
  (tertiary, unclassified, residential, service). Bei einem Neuberechnen nach Abweichen gilt dasselbe. Über den Verkehr abseits der
  Autobahn wissen wir nichts, die Ersparnis ist also bestenfalls; darum die Hürde.
- Ausnahme: wirklich gesperrte Straße. Dann ist die normale Route nicht fahrbar, Zeit- und Straßen-Hürde entfallen und die Umfahrung steht oben.
  Sperrungen der API sind fast immer einzelne Abfahrten (`isBlocked` false), darum strenger Abgleich: 80 % der Meldung höchstens 25 m auf der
  Route. Wer nur vorbeifährt, zählt nicht.
- Technik: `exclude_polygons` als kleine Box (±0,0003°) in der Mitte der Meldung; `use_living_streets` und `use_tracks` 0.
- Abbiegehinweise: Wege von Tür zu Tür bringen `schritte` mit (Valhalla, de-DE). Die App zeigt sie in den Details und den nächsten über der
  Karte. Keine Sprachansage.
- GPS-Stream hielt das Display an (für den Messgang gedacht), das galt mit D-36 auch für jede sichtbare Route. Jetzt nur noch beim Auto.
- Status: umgesetzt.

## D-40 Fahransicht fürs Auto (02.11.)
- Betreiber 02.11.: Ansicht beim Autofahren wie bei Waze und Google Maps. Knopf „Start“ auf der gewählten Autoroute öffnet `lib/fahren.dart`:
  Karte in Fahrtrichtung mit Standort unterhalb der Mitte, Abbiegepfeil und Entfernung oben, Restzeit, Restweg und Ankunft unten, Tempo, blaue
  Route, Stau rot (`form` der Meldung). Ohne Sprachansage: die mag der Betreiber nicht.
- Nur die Kamera bewegt sich, alle 33 ms über einen `ValueListenable`, die Karte baut dafür nicht neu; der Pfeil liegt fest über der Karte. Position und
  Kurs laufen zwischen den GPS-Fixen weich, nah an der Route (35 m) wird der Punkt auf die Route gelegt.
- Display bleibt an, im Hintergrund kein GPS. Neu berechnen wie D-36, alle 3 min Stau und Ankunft frisch; wie D-39 bleibt eine Umfahrung ein Knopf, den man
  antippt, eine gewählte wird nicht zurückgestellt. Restzeit = Anteil des Restwegs an der Fahrzeit.
- Nachtrag: Der Abbiegehinweis richtet sich nach dem Abschnitt der Route, nicht nach dem nächsten Stützpunkt; auf langen geraden Stücken fiel er sonst Hunderte Meter vor dem Abzweig weg.
  Nur die jüngste Routenanfrage zählt, eine verspätete Antwort ersetzt keine gewählte Umfahrung mehr. Der Stand eines ausgefallenen Verkehrsdienstes gilt höchstens 30 min.
- Status: umgesetzt, Betreiber 02.11.: Ansicht passt beim Fahren.

## D-41 RAPTOR-Labels nur noch für erreichte Halte (02.11.)
- Container 272–280 MiB, Ziel < 250. Heap-Profil mit dem echten Fahrplan: 202.277 Halte × 7 Runden × 28 Byte = 38 MB je Router, zwei Router, angelegt erst
  bei der ersten Anfrage. Das war der Sprung von ~160 auf ~250 MB Heap. Eine Anfrage belegt davon im Mittel 5 %, im schlimmsten Fall 18 %.
- Jetzt eine Liste der gesetzten Labels mit Kette je Halt (`head`, `next`): 8 MB je Router in der Messung (400 Zufallsanfragen über ganz DE).
- Geprüft auf demselben geladenen Fahrplan, alter gegen neuen Router: 3000 Zufallsanfragen (auch Ticketfilter, Nur-Bahn, Gehtempo), 954 Verbindungen, 0 Abweichungen.
  Anfragen im Mittel 26 → 25 ms auf dem Laptop.
- Fund und Behebung: `Load` nummerierte Muster und Fahrten nach Map-Reihenfolge, zwei Ladevorgänge desselben Fahrplans gaben bei 0,2–0,4 % der Zufallsanfragen andere Wege,
  teils andere Ankunft. Jetzt feste Reihenfolge wie im Feed, bei gleicher Abfahrt zuerst die früher ankommende Fahrt. Zwei Ladevorgänge: 0 von 3000 Anfragen verschieden.
  Gegen die alte Fassung ändern sich 9 von 3000 Anfragen in der Ankunft, in beide Richtungen, also in der Größe des früheren Rauschens. Load auf dem Laptop 5,3 s.
- Muster mit gleichen Ankunfts- und Abfahrtszeiten (38.483 von 52.308) teilen sich ein Feld: Heap nach dem Laden 146 → 139 MB, bei 3000 Anfragen Ergebnisse bis aufs Byte gleich.
- Status: umgesetzt.

## D-42 Selbstupdate der App über den eigenen Server (05.11.)
- Betreiber wünscht Updates ohne Kabel. Die App fragt beim Start `GET /v1/app`, bei höherem `code` steht das nur in den Einstellungen unter „App-Update“ (kein Banner, Betreiberwunsch 05.11.); geladen wird erst auf Knopfdruck, Installation über den PackageInstaller von Android mit dessen Bestätigung.
- Eigener Server statt öffentlicher GitHub-Releases: nur mit Client-Zertifikat erreichbar, keine Anfrage an Dritte, die App verrät niemandem außer dem eigenen Server, dass sie läuft.
- Sicherheit: Android lässt nur dieselbe Signatur und (mit Code-Vergleich in der App) keine ältere Fassung zu, SHA-256 aus `version.json` prüft die Übertragung. Daten bleiben, nichts wird deinstalliert.
- Neue Berechtigung `REQUEST_INSTALL_PACKAGES`; Android fragt einmal „Apps installieren erlauben“. Kaltstart: Abfrage läuft nebenher, kein Warten. Kein Polling, eine Abfrage je App-Start.
- Veröffentlichen: `scripts/app-release.sh`, Ablage im Add-on-Ordner außerhalb des Images. Ältere Apps merken nichts, ältere Server antworten 404 und die App schweigt.
- Status: umgesetzt.

## D-43 Zufahrten nur für Anlieger zu Fuß als Profil-Schalter (08.11.)
- Fund des Betreibers: Ein kürzerer Weg durch einen Park in Aachen wurde nicht gewählt. Ursache: 28 m Zufahrt, die in den Tiles als destination_only gilt; Valhalla schlägt dafür
  auch bei Fußgängern 600 s auf. Gemessen auf der Strecke: 873 m/12,2 min statt 633 m/8,9 min.
- Entscheidung: Schalter `anlieger` im Profil (Standard aus, also wie bisher), setzt `destination_only_penalty` auf 60 s; 30 und 60 s ergeben den kürzeren Weg, 120 s nicht.
  Ganz ohne Strafe bliebe nichts, was solche Stücke bei gleichem Weg nachrangig macht. Ob eine Zufahrt tatsächlich begehbar ist, weiß nur der Nutzer.
- Gegenprobe mit 60 s auf 10 neutralen Strecken in der Innenstadt: alle unverändert. Die vorab gerechneten Umstiegswege (Job `footpaths`) bleiben wie sie sind.
- Status: umgesetzt.

## D-44 Kopfsteinpflaster umgehen, Tempo je nach Steigung (09.11.)
- Betreiber 09.11.: Longboard durch die Altstadt geschickt. Fußwege sind kein Problem, der Belag ist es; bergab soll schneller gerechnet werden als bergauf.
  RAM darf etwas steigen, solange der Rest nutzbar bleibt und Luft übrig ist.
- Valhalla wertet zu Fuß den Belag befestigter Wege nicht, `sett` landet außerdem im selben Topf wie Gehwegplatten. In OSM steht es aber richtig
  (Köln-Regbez: 4.793 `sett`, 1.131 `cobblestone`).
- Der Job `valhalla` schreibt `belag.ids` neben die Tiles: sortierte Way-IDs (nach renumber, passen also nur zu diesem Stand) mit einer Klasse von 1 (am schlechtesten) bis 5:
  Erde/Gras/Sand und `smoothness` ab very_bad, Schotter, grobes Pflaster (cobblestone, unhewn_cobblestone, `smoothness=bad`), `sett`, Gehwegplatten.
- Betreiber 09.11.: statt Ausweichen mit Umweg-Grenze gilt ein Tempo je Belag. In der App je Klasse „wie normal“, Prozent langsamer oder das Tempo eines anderen Profils (z. B. „Zu Fuß“ auf Schotter);
  an den Server geht nur `belag_kmh` (5 Werte, 0 = wie normal). Der Server rechnet jede Route je Kante (`min(Hang-Tempo, Belag-Tempo)`) und vergleicht die normale Route mit Varianten, die die
  langsamen Klassen umgehen, die langsamste zuerst: Route holen, per `/trace_attributes` die Stücke finden, je Stück eine ~2-m-Box (`exclude_polygons`), neu fragen, höchstens 20 Runden und 200 Boxen,
  60 m um Start und Ziel frei. Die schnellste Variante gewinnt. Valhallas Grenze von 100 Ecken je Anfrage reicht dafür nicht, im Dienst steht sie auf 1000. Ohne `belag_kmh` ändert der Belag nichts.
  `pflaster` und `belag_umweg` aus App 0.2.4–0.2.8 werden angenommen, wirken aber nicht mehr. Der Regler `belag` entfällt in der App für Fuß und eigene Profile; sein gespeicherter Wert wirkt weiter auf Feldwege und den Radtyp.
- Höhendaten (Skadi, 1°-Kacheln der Region, einmalig nach `hoehe/`) gehen in die Tiles; Fahrrad-Kosten und `use_hills` sehen damit Steigungen.
- Profil-Felder `kmh_auf`/`kmh_ab`: linear von `kmh` (0 %) bis zum Wert (±6 %), darüber gleich; fehlt einer, gilt die Ebene. Die Zeit ist dann die Summe je Kante.
  Die App stellt das als Regler in Prozent vom Tempo ein (bergauf −90…0 %, bergab −50…+200 %) und rechnet erst beim Senden in km/h um, damit es dem gemessenen Tempo folgt.
  Eigene Profile haben zusätzlich „Zu Fuß ab x % Steigung“ (Betreiber 09.11., Longboard): bergauf linear von `kmh` bis Gehtempo bei x %, darüber Gehtempo (`kmh_auf` + `hang_voll`).
  Der Messgang setzt die Einstellungen selbst (Betreiber 09.11.; er hat ausdrücklich erlaubt, dass der Server dabei Strecke und Tempo erhält). Am Ende schickt die App die Punkte mit Metern und Sekunden an `POST /v1/messung`,
  der Server gleicht sie per `trace_attributes` (map_snap) mit der Karte ab und antwortet je Steigung und Belagklasse mit Metern und Sekunden; er speichert und protokolliert nichts. Hang und Ebene zählen nur auf unauffälligem Belag.
  Das Ebenen-Tempo ersetzt `kmh` ab 150 m Ebene, ein Hang-Regler braucht ≥ 100 m Steigung, ein Belag ≥ 100 m fast ebene Strecke: dort wird in 10-%-Schritten gerundet, oder bei eigenen Profilen ab 7 km/h das Fuß-Profil gesetzt, wenn im Mittel höchstens 5,6 km/h waren.
  Bei eigenen Profilen ab 7 km/h gilt „zu Fuß ab“: die kleinste Steigung, ab der es im Mittel höchstens 5,6 km/h waren.
- Grenzen: Zu- und Abgangswege zu Haltestellen kommen aus der Matrix, ihre Zeit bleibt eben und ohne Pflaster-Umweg; nur ihre Form umgeht Pflaster. Der Hang ändert die Zeit, nicht
  die Wahl des Wegs (die macht weiter Valhalla, höchstens über `use_hills`).
- Status: umgesetzt.

## D-45 Messgang im Hintergrund (09.11.)
- Betreiber 09.11.: Beim Kalibrieren ging der Bildschirm aus und die Messung war weg. Bisher hing das GPS an der offenen Seite, die Strecke lag nur in deren Speicher.
- `MessDienst` (Vordergrunddienst `location`) nimmt auf und schreibt jede Position sofort in `messung.txt` im App-Ordner, die Seite liest die Datei einmal je Sekunde mit (nur solange sie offen ist).
  Damit läuft die Messung bei Bildschirm aus und geschlossener App weiter, und nach einem Absturz ist das Bisherige noch da. Gelöscht wird die Datei erst, wenn das Ergebnis gespeichert ist oder die Messung verworfen wird.
- Android verlangt für den Dienst eine dauerhafte Benachrichtigung; sie ist leise und hat Pause und Anhalten.
- Bus und Bahn gehören nicht in eine Kalibrierung. Eine Erkennung gibt es beim Kalibrieren bewusst nicht (Betreiber 09.11.), es läuft nur von Hand; zum Ausnehmen gibt es Pause.
- Lässt sich die Strecke nicht auswerten (kein Netz), bleibt sie erhalten: noch einmal auswerten oder nur das Tempo übernehmen.
- Status: umgesetzt.

## D-46 Das Profil lernt aus aufgezeichneten Routen (09.11.)
- Betreiber 09.11.: Kalibrieren nur von Hand und ohne Bus-Erkennung. Auf einer gewählten Route per Hand „Start“: die Fahrt verbessert das Profil, auch wie Bus und Bahn genutzt werden. Ist die App unsicher, ändert sie lieber nichts.
  Der eigene Server darf Position und Tempo dafür verarbeiten, ein Hinweis in der App ist nicht nötig.
- Beenden geht auch über die Benachrichtigung: der Dienst startet eine Flutter-Engine ohne Oberfläche, die auswertet, das Profil speichert und das Ergebnis meldet. An der Zielnähe endet eine Route von selbst.
- `POST /v1/fahrten`: Spur (alle ~4 s: Breite, Länge, Unix-Sekunden, m/s) gegen den Fahrplan des Tages. Am Tempo allein ist ein Bus nicht von einem Longboard zu trennen, deshalb zählen Halte in Fahrplan-Reihenfolge
  (60 m, ±300 s), ein gleichbleibender Versatz zum Fahrplan und ob an Zwischenhalten gestanden wurde. Sicher: ≥ 3 Halte, ≥ 75 % der Halte der Fahrt, Versatz schwankt ≤ 60 s, an mindestens der Hälfte der Zwischenhalte gehalten; oder im Schnitt ≥ 14 m/s.
  Unsicher: nur Teile davon, aber Warten am Einstieg oder Halten unterwegs. Sonst keine Fahrt. Die Spur wird weder gespeichert noch protokolliert.
- Eine unsichere Fahrt: das Profil bleibt für die ganze Aufzeichnung unverändert. Geplante Fahrt erkannt: wird gelernt. Sichere, nicht geplante Fahrt: Rückfrage beim nächsten Öffnen, gelernt erst bei Ja.
- Gelernt wird: (1) Tempo, Hang, Belag aus den eigenen Stücken (Summen je Steigung/Belag im Profil, höchstens 30 km, neue verdrängen alte; daraus rechnet wieder `hangAuswerten`; Kalibrieren setzt den Ausgangspunkt neu).
  Rollende Profile zählen hier erst ab 1,6 m/s, Gehen zur Haltestelle verfälscht sonst das Rolltempo. (2) `bus_min` ab 3 Beobachtungen: die Schwelle, die genutzte und ausgelassene Busse am besten trennt.
  (3) Wartezeit: ab 5 Beobachtungen werden Verbindungen mit längerem Umsteigen ausgeblendet. (4) Strecken je Halt-Paar: ab 3× ausgelassen ausgeblendet, ab 3× genutzt vorgezogen, wenn sie höchstens 3 min später ankommt.
  (3) und (4) wirken in der App auf die gelieferten Verbindungen, es bleibt immer mindestens eine. Von Hand gemiedene Busse ändert das Lernen nicht. In der Profilseite steht das Gelernte und lässt sich zurücksetzen.
- Status: umgesetzt.

## D-47 Abkürzungen: anderer Start, Rückweg, Teilen, Kurzbefehle am Icon (09.11.)
- „Von“ ist wählbar (Suche wie beim Ziel), das Kreuz führt zurück zum eigenen Standort. „Rückweg“ tauscht Start und Ziel; vom eigenen
  Standort aus wird er als Ziel festgehalten (Art `standort`, kein Favorit) und beim nächsten Tausch wieder zum Live-Standort.
- Mit gewähltem Start ist man nicht dort: Abweichungs-GPS, Fahransicht, Aufzeichnung (D-46), knappe Verbindungen (D-31) und der Gehhinweis (D-33)
  sind dann aus, weil sie alle voraussetzen, dass man jetzt am Start steht. Der Server bekommt nur andere Koordinaten, die API bleibt gleich.
- Teilen: Text mit Ankunft und Fahrten über die Android-Auswahl; der Startort kommt bewusst nicht vor.
- Kurzbefehle: die ersten vier Favoriten als dynamische Shortcuts (keine neue Berechtigung). Antippen plant von hier und jetzt. Kennung aus den
  Koordinaten, damit ein angepinnter Kurzbefehl beim Umsortieren nicht auf einen anderen Favoriten wechselt.
- „Spätere Verbindungen“ unter der Liste: neue Suche ab einer Minute nach der spätesten Abfahrt mit Bus oder Bahn, gesetzt als geplante Abfahrtszeit (Zeit-Chip, Kreuz = wieder jetzt). Im Ankunftsmodus wechselt sie auf Abfahrt.

## D-48 Bus-Erkennung: Streckenlänge vorab summieren (09.11.)
- `BenchmarkFahrtenSuchen` (Fahrplan in NRW-Größe, ungünstiger Korridor mit 60 Linien, 7200 GPS-Punkte): 84 % der Zeit gingen in die Streckenlänge,
  die für jeden Kandidaten die ganze Spur ablief. Jetzt einmal aufsummiert und per Binärsuche abgefragt, das Ergebnis der Erkennung bleibt gleich.
- Die App schickt nur etwa jeden vierten GPS-Punkt, echte Anfragen sind also kleiner als die gemessene.

## D-49 App wird auf GitHub gebaut, Updates sagen, was neu ist (11.11.)
- Lokal gibt es keine Flutter-/Android-Werkzeuge mehr (Betreiber: Laptop aufräumen). `scripts/app-release.sh` setzt den Tag `app-v<version>`,
  wartet auf den Workflow app.yml (Tests, signierter Build, APK-Prüfung), lädt die APK per `gh` und legt sie wie bisher auf den Server.
- `app/neu.txt` beschreibt kurz, was das Update ändert, und ist Pflicht. Das Skript schreibt den Text als `hinweis` in version.json,
  `/v1/app` gibt ihn weiter, die App zeigt ihn unter „Was ist neu“ vor der Installation. Ältere Apps und Server ignorieren das Feld.
