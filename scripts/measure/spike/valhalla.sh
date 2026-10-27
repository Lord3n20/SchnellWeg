#!/bin/bash
# Valhalla-Probebau StädteRegion: Fußgänger/Rad/Auto-Tiles, concurrency 4, ohne Höhe.
set -e
mkdir -p ${VH:-/w/vh}; cd ${VH:-/w/vh}
log(){ echo "$(date +%T) $*" >> ${VLOG:-/w/valhalla.log}; }
valhalla_build_config --mjolnir-tile-dir ${VH:-/w/vh}/tiles --mjolnir-tile-extract ${VH:-/w/vh}/tiles.tar  \
  --mjolnir-timezone ${VH:-/w/vh}/tz.sqlite --mjolnir-admin ${VH:-/w/vh}/admin.sqlite --mjolnir-concurrency 4 > valhalla.json
S=$(date +%s); valhalla_build_admins -c valhalla.json ${PBF:-/w/raw/sr.osm.pbf} >> b.log 2>&1; log "admins $(( $(date +%s)-S )) s"
S=$(date +%s); valhalla_build_timezones > tz.sqlite 2>> b.log || true; log "tz $(( $(date +%s)-S )) s"
S=$(date +%s); valhalla_build_tiles -c valhalla.json ${PBF:-/w/raw/sr.osm.pbf} >> b.log 2>&1
log "tiles $(( $(date +%s)-S )) s"
S=$(date +%s); valhalla_build_extract -c valhalla.json -v >> b.log 2>&1; log "extract $(( $(date +%s)-S )) s $(du -sm tiles tiles.tar | tr '\n' ' ')"
log "cgroup peak $(cat /sys/fs/cgroup/memory.peak 2>/dev/null)"
valhalla_service valhalla.json 2 > srv.log 2>&1 & P=$!
sleep 5; grep VmRSS /proc/$P/status >> ${VLOG:-/w/valhalla.log}
python3 /w/bench.py valhalla >> ${VLOG:-/w/valhalla.log} 2>&1
grep -E "VmRSS|VmHWM" /proc/$P/status >> ${VLOG:-/w/valhalla.log}; kill $P; log "service fertig"
