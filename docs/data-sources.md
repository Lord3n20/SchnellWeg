# Datenquellen und externe Dienste

Stand 27.10.2026: **erste Prüfung**, noch nicht beim Betreiber vollständig verifiziert. Freigegeben wird erst
nach Phase 0. Externe Anfragen gehen immer von der Heim-IP aus; das ist akzeptiert und pro Dienst vermerkt.

Status: ✅ freigegeben · ⏳ in Prüfung · ❓ Rückfrage an den Betreiber · ❌ abgelehnt

| Quelle | Betreiber / Art | Lizenz | Registrierung | Übertragen | Status |
|---|---|---|---|---|---|
| gtfs.de `de_nv`/`de_rv` (statisch, 277 MB) | gtfs.de, verantwortlich Dr. Patrick Brosi (Impressum, Rechtsform nicht genannt); Daten aus NeTEx von DELFI e.V. | **CC BY 4.0** (Feed-Seite, geprüft 27.10.) | nein | IP, Abruf 1×/Tag mit ETag | ✅ |
| DELFI GTFS über opendata-oepnv.de | DELFI e.V. (Länder/Verbünde, öffentlich) | prüfen (CC BY 4.0 erwartet) | **ja** | Account-Daten, IP | ❓ F-06 |
| gtfs.de GTFS-RT `realtime-free.pb` (27 MB) | gtfs.de | **CC BY-SA 4.0** | nein | IP, Abrufmuster (nur „App offen“, keine Routen) | ✅ D-15 (in routing-core); ASEAG 27.10. mittags 93 % / 80 %, abends 89 % / 57 % (messungen/) |
| Transitous API | gemeinnütziges Community-Projekt, Server bei spline (FU Berlin) | keine eigene; Quellen-Lizenzen + OSM-Attribution, Link auf transitous.org/sources | nein | IP, URL (bei uns nur Halte-IDs + Zeit), UA; Logs 2 Tage | ❓ F-05 (UA mit Kontakt, Open-Source-Pflicht) |
| DB Timetables (API Marketplace) | DB InfraGO, Staatskonzern | CC BY 4.0 (laut Dritten, beim Betreiber prüfen) | **ja** (Konto, Client-ID/Secret) | IP, Bahnhofs-EVA + Stunde | ❓ nur bei Bedarf |
| HAFAS / db-vendo-client | inoffiziell | keine Erlaubnis | – | – | ❌ |
| AVV-HAFAS `auskunft.avv.de/gate` (so macht es Öffi) | AVV, nicht öffentlich dokumentiert, geheimer Auth-Schlüssel | keine | Schlüssel aus App/Website | Start/Ziel an AVV | ❌ (Vorgabe „keine HAFAS“), Rückfrage 27.10. |
| OVapi GTFS / GTFS-RT (NL) | openOV, Community | prüfen | nein | IP | ⏳ |
| SNCB/NMBS GTFS(-RT) (BE) | SNCB, öffentlich | eigene Lizenz, Antragsformular | **wohl ja** | prüfen | ⏳ |
| TEC GTFS (BE, Wallonien) | OTW, öffentlich | prüfen | prüfen | IP | ⏳ |
| OSM-Extrakte (Geofabrik) | Geofabrik GmbH, kommerziell (Downloads frei) | ODbL | nein | IP, 1×/Tag | ⏳ |
| Höhendaten (Copernicus DEM GLO-30 bzw. Tilezen/Skadi auf AWS Open Data) | ESA/EU bzw. Amazon als Hoster | Copernicus-Lizenz (frei, Attribution) / gemischt | nein | IP, einmalig | ⏳ |
| Protomaps-Build (PMTiles) | Protomaps (Brandon Liu), Community/kommerziell | **ODbL Produced Work**, OSM-Namensnennung (docs.protomaps.com/basemaps/downloads, geprüft 27.10.; eigene Kopien statt Verlinken empfohlen) | nein | IP, HTTP-Range-Abrufe, wöchentlich | ✅ D-17 |
| Autobahn-API (verkehr.autobahn.de) | Autobahn GmbH des Bundes | **beim Betreiber nichts gefunden**, nur bund.dev (Dritte) | nein | IP | ❓ Grenzfall (Phase 7) |
| Mobilithek / Straßen.NRW DATEX II | BMDV, öffentlich | je Angebot | **ja** | Account, IP | ❓ (Phase 7) |
| RWTH Maps (maps.rwth-aachen.de), `getBuildings.asp` | RWTH, öffentlich, interne Schnittstelle | **keine Lizenz/Schnittstelle genannt**, Karte auf Google Maps API | nein | Server: IP, 1×/Tag; App: IP + Suchtext (kein Standort) | ⚠️ Abweichung D-18 (vom Betreiber gewollt) |
| RWTH-Gebäude aus OSM | OSM | ODbL | nein | – (im OSM-Extrakt) | ✅ teilweise: 57 Objekte mit Uni-Bezug im Zentrum, 54 mit Name, keine Gebäudenummern |
| NDW Open Data (NL) | Rijkswaterstaat/NDW, öffentlich | CC0 erwartet | nein | IP | ⏳ (Phase 7) |

## Detailnotizen

### Transitous (geprüft 27.10. auf transitous.org/api und /privacy)
- Nutzung: Projekt muss Open Source sein, keine kommerzielle Nutzung, ressourcenintensive Dienste vorher absprechen,
  Schwelle ~720 Anfragen/24 h für `/plan`, `/one-to-many`, `/one-to-all`. Dienst nach bestem Bemühen.
- User-Agent: „Name der Anwendung, Version, **Kontaktmöglichkeit**“.
- Datenschutz: Logs mit IP, Zeit, URL (Start/Ziel, Zeit, Optionen), UA; Löschung nach 2 Tagen; Rechtsgrundlage
  berechtigtes Interesse (Betrieb/Fehlerbehebung). Weitergabe: keine Angabe → nachfragen oder als „keine Weitergabe
  erwähnt“ werten (❓).
- Dokumentation: Betreiber selbst (MOTIS-OpenAPI).

### gtfs.de Echtzeit (geprüft 27.10. auf gtfs.de/de/realtime)
- URL `https://realtime.gtfs.de/realtime-free.pb`, Beta, alle 10 s neu, TripUpdates + ServiceAlerts, passt zu den
  statischen gtfs.de-Feeds. HEAD: 27 059 288 Byte, ETag + Last-Modified vorhanden.
- Genannte Quellen: VVO, VRS, VBN, VRR, NVBW, VAG, VBB, VRN/RNN, Stadtwerke Münster, „diverse DELFI-SIRI-Quellen“.
  ASEAG/AVV nicht ausdrücklich genannt → Messung in Phase 0.
- CC BY-SA: Attribution in der App; Share-Alike betrifft abgeleitete Datenbanken, die wir nicht veröffentlichen.

### Öffi (public-transport-enabler, angesehen 27.10., Commit 74ca2f7)
- Kein eigener Server: die App fragt direkt die Auskunftssysteme der Betreiber ab.
- Aachen: `AvvAachenProvider` → HAFAS-mgate `https://auskunft.avv.de/gate`, API 1.69, Client `AVV_AACHEN/WEB`,
  Auth-Schlüssel nicht im Repo (geheime Eigenschaft). DB: `app.services-bahn.de/mob` (Schnittstelle des DB Navigator).
- Beides inoffiziell und mit Start/Ziel beim Betreiber → widerspricht CLAUDE.md. Die Echtzeit, die Öffi so bekommt,
  liefert für ASEAG auch der offene gtfs.de-Feed (siehe messungen/2026-10-27-rt-abdeckung.md).

### RWTH-Gebäude (geprüft 27.10.)
- RWTH Maps: keine API, kein Download, keine Lizenz; Basiskarte Google Maps → als Quelle ungeeignet.
- Ersatz: OSM-Gebäude mit Namen (Audimax, Institute, …) in die Adresssuche + Enzos eigene Gebäude mit
  Nummer/Kurzname in `config/local.yaml` (gitignored).
