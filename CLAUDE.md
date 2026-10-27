# ÖPNV-Routing Aachen – harte Rahmenbedingungen

Projekt `SchnellWeg` (öffentliches Repo Lord3n20/SchnellWeg; bis 27.10. „acwegweiser“, dann „RWaysTH“, seit 27.10. abends SchnellWeg; Add-ons in server-homeassistant). Antworten und Doku auf Deutsch.
Die volle Spezifikation stammt vom 27.10.2026; diese Datei hält nur das Verbindliche fest.
Abweichungen nie stillschweigend: begründet in `docs/decisions.md`. Widersprechen sich zwei Vorgaben → Betreiber fragen.
Konkrete Zugänge, Domain, IPs und private Dienste stehen nur in `CLAUDE.local.md` (gitignored).

## Server (Betrieb nur hier)
- Dell OptiPlex 9020 USFF, **i7-4770S** (Haswell, 4 Kerne/8 Threads, 3,1 GHz), 7,8 GB RAM, 2,5 GB Swap,
  ~340 GB frei auf /data. Home Assistant OS, Bare Metal, `amd64`.
- Läuft daneben: HA Core (~550 MB), Supervisor (~590 MB), ein Spieleserver und weitere Dienste, NPM, SSH.
  Stand 27.10.: ~6,0 GB „available“, ~1 GB Swap belegt.
- Deployment ausschließlich als HA-Add-ons im Repo `Lord3n20/server-homeassistant` (Aufbau wie vorhandene Apps:
  `<name>-app/` mit config.yaml, Dockerfile, README.md, translations/). Kein Compose.
- Alle Images/Basis-Images mit festem Tag **und** Digest, nie `latest`.

## RAM
| | Ziel | Harte Grenze |
|---|---|---|
| Alle neuen Add-ons im Betrieb | < 250 MB | < 500 MB |
| Einmal-Job Spitze | so niedrig wie möglich | ≤ 2 GB (Option) **und** ≤ MemAvailable − 1 GB |
- Speichersparsam entwickeln (mmap, Streaming), nicht eng limitieren. Go: `GOMEMLIMIT` als Sicherheitsnetz.
- Jobs prüfen freien Speicher vor Start, überwachen sich (RSS/cgroup), brechen sauber ab, protokollieren Spitze + Laufzeit.

## CPU
- Jobs: `nice -n 19`, `ionice -c3`, max. 4 Threads (`GOMAXPROCS=4`, Valhalla `concurrency` ≤ 4) → der Spieleserver hat immer ≥ 4 Threads frei.
- Jobs nacheinander, nie parallel. Im Betrieb keine Dauerlast, kein Polling ohne offene App. Der Spieleserver darf nie ruckeln.

## Datenjobs
- Eigene Add-ons ohne `boot: auto`, beenden sich selbst. Gestartet nur per HA-Automation (`hassio.addon_start`),
  kein Cron/Scheduler im Container. Standard 04:00, nur wenn niemand auf dem Spieleserver online; sonst +30 min bis 07:00.
- Unveränderte Eingänge (ETag/Prüfsumme) → überspringen. Validierung vor Umschalten, atomarer Symlink-Wechsel,
  vorheriger Stand bleibt als Rollback. Ergebnis als HA-Benachrichtigung (keine externen Dienste).
- Große, reproduzierbare Daten nicht in HA-Backups.

## Datenschutz
- Nur offene, frei nutzbare Quellen (Lizenz beim Betreiber geprüft, in `docs/data-sources.md`). Grenzfälle und
  Registrierungen nur nach Rückfrage. Keine Google/TomTom/HERE, keine inoffiziellen Schnittstellen (z. B. HAFAS).
- Nach außen: ÖPNV nur mit Haltestellen-/Fahrt-IDs, nie Koordinaten; Verkehrsdaten nur regionsweit; Auto/Rad lokal.
- Kein Logging von Anfragen, Zielen, Standorten (auch nicht in Fehlermeldungen); NPM `access_log off;`.
  Logs nur Fehlertyp, Quellenstatus, Zeitwerte.
- App: keine Play Services, kein Firebase/Analytics/Tracking (APK-Prüfung bricht Build ab).
- Keine Zugangsdaten im Repo (Passwortmanager). Persönliche Orte nur in `config/local.yaml` (gitignored);
  im Repo nur neutrale Orte (Aachen Hbf, Elisenbrunnen …).

## Zeitbudgets
- Echtzeitquelle: 6 s (max. 1 Retry, wenn Zeit reicht), Gesamt bis final: 8 s. Caches nur RAM, alle mit Obergrenze.

## Zielwerte (Abnahme)
| Messgröße | Ziel | Harte Grenze |
|---|---|---|
| RAM gesamt Betrieb (neue Add-ons) | < 250 MB | < 500 MB |
| RAM-Spitze Einmal-Job | minimal | ≤ 2 GB und ≤ frei − 1 GB |
| RAPTOR pro Anfrage (OptiPlex) | < 50 ms | < 100 ms |
| Valhalla erste/letzte Meile | < 50 ms | < 100 ms |
| Adresssuche | < 30 ms | < 50 ms |
| Erste Anzeige Handy (vorläufig) | < 0,5 s | < 1 s |
| Finale Empfehlung Handy | < 4 s | 10 s |
| App-Kaltstart bis Startbildschirm | < 1 s | < 2 s |
| CPU Leerlauf | ≈ 0 | kein Polling ohne Nutzung |
Handy-Zeiten: Median + p95, im Mobilfunk. Jede Phase endet mit Tests (inkl. Referenzstrecken), README, Messungen, Abgleich.
