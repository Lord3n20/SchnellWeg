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
- Status: angenommen. Stichprobe abends/Wochenende/Störung wiederholen.

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
  (Mindestumstieg 60 s bleibt), damit kein zweiter Fußweg-Datensatz nötig ist.
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
