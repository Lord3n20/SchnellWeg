# Phase 0 · gtfs.de-RT abrufen und filtern auf dem OptiPlex (27.10.2026, 13:53)

`server/cmd/rtprobe` (Go, statisch, Commit 67286d9), im SSH-Add-on auf dem Server, `nice -n 19 ionice -c3`,
`GOMAXPROCS=4 GOMEMLIMIT=100MiB`. Filter: 17 656 trip_ids der vier Aachener Unternehmen. Spieleserver leer.

| | Größe | Einträge | behalten | Wand | CPU | RSS-Spitze (VmHWM) |
|---|---|---|---|---|---|---|
| Abruf 1 | 31,8 MB | 121 096 | 2 203 | 792 ms | 566 ms | 19,7 MB |
| Abruf 2 (+30 s) | 31,8 MB | 120 985 | 2 199 | 777 ms | 465 ms | 19,7 MB |
| Abruf 3 (+30 s) | 31,6 MB | 120 903 | 2 199 | 915 ms | 662 ms | 25,4 MB |
| aus Datei (×3) | 31,6 MB | 120 903 | 2 199 | 32–33 ms | 37–39 ms | 15–21 MB |

- Reines Filtern: **33 ms**. Der Rest des Abrufs ist Download + TLS (~0,5 s CPU je 32 MB).
- RAM: **< 26 MB Spitze** – Ziel „< 30 MB zusätzlich“ (plan.md §3) erfüllt, weil nur Regions-TripUpdates dekodiert werden.
- Feed wächst über den Tag (Vormittag 28 MB, Mittag 32 MB).
- Folgen für D-05: Abruf ≤ 1 s passt ins 6-s-Budget; bei offener App alle 30 s ≈ 1 s CPU/min auf einem Kern → vertretbar,
  aber nur bei offener App. `If-None-Match` lohnt (Feed nur alle 10 s neu).
- Laptop-Vergleich (Core Ultra 7, Datei): 18 ms.
