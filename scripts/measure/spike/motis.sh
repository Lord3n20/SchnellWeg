#!/bin/sh
# MOTIS-Probe StädteRegion: Import messen, Server starten, Abfragen messen.
set -e
cd /w/mo
log(){ echo "$(date +%T) $*" >> /w/motis.log; }
apt-get update -qq && apt-get install -y -qq time python3 ca-certificates >/dev/null
[ -f config.yml ] || { /w/motis/motis config /w/raw/sr.osm.pbf /w/raw/sr_gtfs.zip >> /w/motis.log 2>&1; sed -i "/^tiles:/,/^  flush_threshold/d" config.yml; }
if [ "$NOOSM" = 1 ]; then sed -i "/^osm:/d; s/^street_routing: true/street_routing: false/; s/^geocoding: true/geocoding: false/; s/^reverse_geocoding: true/reverse_geocoding: false/" config.yml; fi
nice -n 19 ionice -c3 /usr/bin/time -f "import %e s, RSS-Spitze %M KB" -a -o /w/motis.log /w/motis/motis import >> imp.log 2>&1
log "daten $(du -sm data | cut -f1) MB"
/w/motis/motis server > srv.log 2>&1 & P=$!
for i in $(seq 60); do python3 -c "import urllib.request;urllib.request.urlopen('http://127.0.0.1:8080/',timeout=2)" 2>/dev/null && break; sleep 2; done
log "server bereit nach ~$((i*2)) s"; grep -E "VmRSS" /proc/$P/status >> /w/motis.log
python3 /w/bench.py ${BENCH:-motis} >> /w/motis.log 2>&1
grep -E "VmRSS|VmHWM" /proc/$P/status >> /w/motis.log
log "cgroup peak $(cat /sys/fs/cgroup/memory.peak 2>/dev/null)"; kill $P; log fertig
