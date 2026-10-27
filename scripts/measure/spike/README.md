# Phase-0-Spike: Valhalla und MOTIS auf dem OptiPlex (StädteRegion)

Einmalige Messskripte, laufen in Wegwerf-Containern auf dem Server (`docker run --rm --memory 2g --cpus 4`,
Arbeitsordner `/share/oepnv-spike` → `/w`; für `docker run -v` auf dem Host-Pfad
`/mnt/data/supervisor/share/oepnv-spike`, weil das HA-Terminal `/share` nur im eigenen Container sieht). Ergebnisse:
`docs/messungen/2026-10-27-spike-valhalla-motis.md`, `docs/messungen/2026-10-27-fusswege-valhalla.md`.

1. OSM: Geofabrik `koeln-regbez-latest.osm.pbf` (md5 geprüft), Ausschnitt StädteRegion-Box
   `5.95,50.48,6.45,50.93` mit `osmconvert --complete-ways --hash-memory=400` (debian:13-slim, osmctools).
   `osmium extract` (smart/complete_ways) überschreitet 2 GB → OOM.
2. GTFS: `../gtfs_bbox.py de_nv.zip <box> sr_gtfs.zip` (alle Fahrten mit ≥ 1 Halt in der Box).
3. `valhalla.sh` im Image `ghcr.io/valhalla/valhalla:3.9.1`, `motis.sh` in debian:13-slim mit MOTIS v2.11.3.
4. `bench.py valhalla|motis` misst Latenzen gegen den lokal gestarteten Dienst (neutrale Orte).
5. Fußwege: `raptorprobe -gtfs … -pairs-out pairs.csv` (Laptop) → zum Server (Entwurfs-Release + Kurzzeit-URL) →
   im Valhalla-Container `footpaths.py pairs.csv footpaths.csv 2` → zurück → `raptorprobe … -footpaths footpaths.csv`.
6. Erste/letzte Meile: `raptorprobe … -access-out access.json` → `bench.py access /w/access.json`.
