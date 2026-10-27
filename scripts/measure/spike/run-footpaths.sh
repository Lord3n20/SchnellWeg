#!/bin/sh
# Spike Fußwege auf dem Server: OSM laden, StädteRegion ausschneiden, Valhalla bauen, Fußwege rechnen.
cd /share/oepnv-spike; L=/share/oepnv-spike/run.log; : > $L
log(){ echo "$(date +%T) $*" >> $L; }
log start
[ -s raw/koeln.osm.pbf ] || nice -n 19 ionice -c3 curl -sfL -o raw/koeln.osm.pbf https://download.geofabrik.de/europe/germany/nordrhein-westfalen/koeln-regbez-latest.osm.pbf
curl -sfL https://download.geofabrik.de/europe/germany/nordrhein-westfalen/koeln-regbez-latest.osm.pbf.md5 | awk '{print $1"  raw/koeln.osm.pbf"}' | md5sum -c >> $L 2>&1 || { log md5-fehler; exit 1; }
log "pbf $(du -m raw/koeln.osm.pbf | cut -f1) MB"
docker run --rm --memory 2g --cpus 4 -v /mnt/data/supervisor/share/oepnv-spike:/w debian:13-slim sh -c \
  'apt-get -qq update >/dev/null && apt-get -qq install -y osmctools >/dev/null && nice -n 19 ionice -c3 osmconvert /w/raw/koeln.osm.pbf -b=5.95,50.48,6.45,50.93 --complete-ways --hash-memory=400 -o=/w/raw/sr.osm.pbf' >> $L 2>&1
log "sr $(du -m raw/sr.osm.pbf | cut -f1) MB"
docker run --rm --memory 2g --cpus 4 -v /mnt/data/supervisor/share/oepnv-spike:/w --entrypoint /bin/bash ghcr.io/valhalla/valhalla:3.9.1@sha256:4fd114eb1d26b8fc2f43bcb3df234c1b63a0bf13c277405e5943d5361dc575de -c '
cd /w; mkdir -p vh; cd vh
valhalla_build_config --mjolnir-tile-dir /w/vh/tiles --mjolnir-tile-extract /w/vh/tiles.tar --mjolnir-admin /w/vh/admin.sqlite --mjolnir-concurrency 4 > valhalla.json
S=$(date +%s); nice -n 19 ionice -c3 valhalla_build_admins -c valhalla.json /w/raw/sr.osm.pbf > b.log 2>&1
nice -n 19 ionice -c3 valhalla_build_tiles -c valhalla.json /w/raw/sr.osm.pbf >> b.log 2>&1
nice -n 19 ionice -c3 valhalla_build_extract -c valhalla.json >> b.log 2>&1
echo "$(date +%T) bau $(( $(date +%s)-S )) s, tiles $(du -sm tiles.tar | cut -f1) MB, cgroup peak $(cat /sys/fs/cgroup/memory.peak)" >> /w/run.log
nice -n 19 valhalla_service valhalla.json 2 > srv.log 2>&1 & P=$!; sleep 5
for th in 2 4; do nice -n 19 python3 /w/footpaths.py /w/pairs.csv /w/footpaths.csv $th >> /w/run.log 2>&1; done
grep -E "VmHWM" /proc/$P/status >> /w/run.log; kill $P'
log fertig
