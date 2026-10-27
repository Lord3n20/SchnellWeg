# Erste/letzte Meile über Valhalla im Betrieb (27.10.2026, abends)

routing-core 0.1.1–0.1.5 auf dem Server, Valhalla-App mit 2 Threads, Cache 32 MB. Messung von außen über den
Reverse Proxy (Client-Zertifikat), 8 Referenzpaare × 5 Uhrzeiten × 3 Wiederholungen = 120 Anfragen, nacheinander.
`walk_ms` = Halte-Suche + beide Valhalla-Matrizen (Start und Ziel gleichzeitig), vom Server selbst gemessen.

| Stand | walk_ms Median | p95 | max | RAPTOR Median |
|---|---|---|---|---|
| 0.1.1 (erste Anbindung) | 54,0 ms | 67,4 ms | 81,5 ms | 4,1 ms |
| 0.1.2 (Halte-Suche schneller) | 50,7 ms | 67,7 ms | 71,8 ms | 4,4 ms |
| 0.1.4 (ohne Keep-Alive) | 58,6 ms | 72,8 ms | 79,0 ms | 4,3 ms |
| 0.1.5, CPU durch Hintergrundlast hochgetaktet | **18,7 ms** | **27,0 ms** | 28,2 ms | 1,6 ms |

Ziel < 50 ms, Grenze < 100 ms: ohne Hochtakten knapp verfehlt (Median), mit Hochtakten deutlich erreicht.

## Ursache: CPU-Takt

- Dieselbe Matrix braucht von außen 20/35 ms (Start/Ziel), direkt auf dem Server hintereinander 5/8 ms – mit
  derselben routing-core-Instanz. Mit einem Kern Hintergrundlast (`nice -n 19`) auch von außen 5/8 ms.
- Der Server taktet mit `intel_cpufreq` + `schedutil` (0,8–3,9 GHz, Haswell: ein Takt für alle Kerne). Eine
  einzelne Anfrage dauert zu kurz, als dass der Regler hochtaktet; Tests auf dem Server erzeugen nebenbei Last
  (Shell, curl) und messen daher zu schnell.
- Ausgeschlossen (je gemessen): Netzweg zwischen den Containern, DNS (< 1 ms), Verbindungsaufbau (< 0,5 ms),
  Inhalt der Anfrage (aufgezeichnete Anfrage per curl gleich schnell), Tile-Cache (128 statt 32 MB: unverändert).
- Messwerkzeug: `"debug": true` in der Anfrage liefert je Valhalla-Aufruf Zahl der Halte, DNS, Verbindung,
  erste Antwort, gesamt (nur Zeiten).

## Nebenbefunde (behoben)

- Halte-Suche: `Near` lief über alle 24 707 Halte (lokal 1,1 ms) → Breiten-Index 0,04 ms; `PerRoute` mit Map
  0,22 ms → Array 0,05 ms. Ergebnisse von `raptorprobe -per-route -legs` unverändert.
- Valhallas HTTP-Server wird auf wiederverwendeten Verbindungen mit jeder Anfrage langsamer (curl über eine
  Verbindung: 15 → 27 ms nach 10 Anfragen). routing-core baut daher je Aufruf eine neue Verbindung auf.

## Offen

- Entscheidung Betreiber: Regler `performance` (oder höherer Mindesttakt) auf dem Server – betrifft den ganzen
  Host, nicht nur die Add-ons.
