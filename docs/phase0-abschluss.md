# Phase 0 · Abschluss (27.10.2026)

Alle Werte auf dem OptiPlex (Server) gemessen, Details in `messungen/`.

## Zielwerte

| Messgröße | Ziel / Grenze | Gemessen | |
|---|---|---|---|
| RAPTOR je Anfrage | < 50 / < 100 ms | Median 1,0 ms, p95 ≤ 2,1 ms (Region + DE-Regional- + Fernzüge) | ✅ |
| RAPTOR RAM | Teil von < 250 MB | ~40 MB (Laden 1,7 s, Spitze 92 MB) | ✅ |
| Valhalla erste/letzte Meile | < 50 / < 100 ms | Median 18 ms, p95 ~59 ms | ⚠️ p95 über Ziel, unter Grenze |
| Valhalla-Dienst RAM | Teil von < 250 MB | 150–230 MB | ⚠️ mit RAPTOR + Echtzeit knapp über 250 MB → Cache verkleinern (Phase 1) |
| Valhalla-Bau (Job) | ≤ 2 GB | 1,0 GB StädteRegion, ~2 GB Regbez. Köln (Page Cache) | ⚠️ volle Region im Job genau messen |
| Fußweg-Job | ≤ 2 GB | 7 s, ~230 MB | ✅ |
| Echtzeit-Abruf | 6 s Budget | 0,8 s, < 26 MB | ✅ |
| ASEAG-Echtzeit vorhanden | – | 93 % der laufenden Fahrten im offenen Feed | ✅ (Stichprobe Di mittags) |
| Adresssuche, Handy-Zeiten | – | noch nicht gebaut | Phase 1+ |

## Entscheidungen
- Angenommen: D-01 Go, D-02 eigenes RAPTOR, D-05/D-09 Echtzeit gtfs.de, D-07 Auto/Rad offline DE, D-08 Abdeckung,
  D-11 Valhalla auf dem Server, D-12 Fußwege (Gehtempo einstellbar, Standard 4,3 km/h).
- Technisch, ohne Rückfrage übernommen: D-10 osmconvert statt osmium (osmium sprengt 2 GB).
- D-04: Daten im `addon_config` des Job-Add-ons mit `backup_exclude` (27.10.) – erledigt.
- D-03 Images per GitHub-CI nach GHCR: angenommen, Repo ist seit 27.10. öffentlich.
- D-06 Ampel-Schwellen: Phase 4.

## Noch offen vor/in Phase 1
- ~~Ticketfilter~~ erledigt (27.10., Phase 1): `de_fv.zip:fern` markiert den ganzen Feed, in anderen Feeds gelten
  ICE/EC/ECE/ECM/EN/NJ/RJ(X)/THA/TGV/ES(T)/FLX als Fernverkehr. IC17/35/56/87 aus de_rv bleiben (Tarifanerkennung,
  teils nur abschnittsweise). API schließt Fernverkehr aus, außer `"fern": true`; Fahrten tragen `fern` in der Ausgabe.
- Echtzeit-Stichprobe abends, am Wochenende und bei einer Störung wiederholen.
- DELFI-Registrierung: vorher klären, welche Daten verlangt werden.
- Persönliche Referenzorte in `config/local.yaml` ergänzen (RWTH Maps ist schon abgelehnt, Gebäude aus OSM).
