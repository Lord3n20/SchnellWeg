# Fußwege zwischen Halten per Valhalla-Matrix (one-to-many je Quelle). Aufruf im Valhalla-Container:
#   footpaths.py pairs.csv footpaths.csv [threads] [km/h]
# pairs.csv kommt aus `raptorprobe -pairs-out`; Ausgabe from_id,to_id,sec,m (leer = kein Weg).
import csv, json, sys, time, urllib.request, statistics
from concurrent.futures import ThreadPoolExecutor
U = "http://127.0.0.1:8002/sources_to_targets"
KMH = float(sys.argv[4]) if len(sys.argv) > 4 else 4.3  # vorsichtig für Umstiege (Valhalla-Standard 5,1)
src = {}
for r in csv.DictReader(open(sys.argv[1])):
    src.setdefault((r["from_id"], r["from_lat"], r["from_lon"]), []).append((r["to_id"], r["to_lat"], r["to_lon"]))
def one(item):
    (a, la, lo), tg = item
    body = {"sources": [{"lat": float(la), "lon": float(lo)}], "targets": [{"lat": float(x), "lon": float(y)} for _, x, y in tg],
            "costing": "pedestrian", "costing_options": {"pedestrian": {"walking_speed": KMH}}}
    t = time.perf_counter()
    try:
        d = json.loads(urllib.request.urlopen(urllib.request.Request(U, json.dumps(body).encode(), {"Content-Type": "application/json"}), timeout=30).read())
        row = d["sources_to_targets"][0]
    except Exception:
        row = [{}] * len(tg)  # z. B. Halt nicht anschließbar → alle Paare ohne Weg
    ms = (time.perf_counter() - t) * 1000
    return [(a, b, c.get("time"), round(c["distance"] * 1000) if c.get("distance") is not None else None) for (b, _, _), c in zip(tg, row)], ms
t0 = time.time(); lat = []; n = ok = 0
with open(sys.argv[2], "w", newline="") as f, ThreadPoolExecutor(int(sys.argv[3]) if len(sys.argv) > 3 else 2) as ex:
    w = csv.writer(f); w.writerow(["from_id", "to_id", "sec", "m"])
    for rows, ms in ex.map(one, src.items()):
        lat.append(ms)
        for a, b, s, m in rows:
            n += 1; ok += s is not None
            w.writerow([a, b, "" if s is None else s, "" if m is None else m])
lat.sort()
print(f"{len(src)} Quellen, {n} Paare, {ok} mit Weg, {time.time()-t0:.1f} s; je Anfrage median {statistics.median(lat):.1f} ms, p95 {lat[int(len(lat)*0.95)-1]:.1f} ms")
