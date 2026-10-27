# Erzeugt die Laufzeit-Konfiguration des Valhalla-Dienstes aus der Standardkonfiguration (stdin) → stdout.
#   valhalla_build_config | python3 config.py <tiles.tar> <cache_mb>
# Kleiner Tile-Cache (Tiles liegen per mmap im Page Cache), keine Zeitzonen/Verkehr, und kein Protokoll,
# damit keine Koordinaten aus Anfragen oder Fehlermeldungen irgendwo landen.
import json, sys
c = json.load(sys.stdin)
m = c["mjolnir"]
m["tile_extract"] = sys.argv[1]
m["tile_dir"] = "/nonexistent"
m["traffic_extract"] = "/nonexistent/traffic.tar"
m["admin"] = "/nonexistent/admin.sqlite"
m["timezone"] = "/nonexistent/tz.sqlite"
m["max_cache_size"] = int(sys.argv[2]) * 1000 * 1000
m["use_lru_mem_cache"] = True
m["lru_mem_cache_hard_control"] = True
c["logging"] = {"type": "file", "file_name": "/dev/null", "color": False}
for k in ("loki", "thor", "odin", "meili", "mjolnir"):
    c[k].setdefault("logging", {}).update({"type": "file", "file_name": "/dev/null", "color": False, "long_request": 1e12})
c["httpd"]["service"]["listen"] = "tcp://*:8002"
json.dump(c, sys.stdout, indent=1)
