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
