#!/bin/bash
# Startet valhalla_service auf dem aktuellen Tile-Stand von routing-jobs. Optionen aus /data/options.json.
set -eu
opt() { python3 -c "import json,sys;print(json.load(open('/data/options.json')).get('$1','$2'))"; }
THREADS=$(opt threads 2); CACHE=$(opt cache_mb 32)
TAR=$(ls -d /addon_configs/*_routing_jobs/valhalla/current/tiles.tar 2>/dev/null | head -1 || true)
if [ -z "$TAR" ]; then
  echo "Fehler: keine Tiles gefunden – erst das Job-Add-on mit job osm,valhalla laufen lassen"
  exit 1
fi
STAND=$(basename "$(readlink -f "$(dirname "$TAR")")")
valhalla_build_config | python3 /usr/local/share/rwaysth/config.py "$TAR" "$CACHE" > /tmp/valhalla.json
echo "Valhalla: Stand $STAND, $(du -m "$(readlink -f "$TAR")" | cut -f1) MB Tiles, $THREADS Threads, Cache $CACHE MB"
# Protokoll des Dienstes geht nach /dev/null (Datenschutz); stdout/stderr nur für Absturzmeldungen.
exec valhalla_service /tmp/valhalla.json "$THREADS" > /dev/null
