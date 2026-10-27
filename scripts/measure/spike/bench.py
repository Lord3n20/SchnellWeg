# Latenzmessung gegen laufenden Dienst. Aufruf: bench.py valhalla|access|motis
import json, sys, time, urllib.parse, urllib.request, statistics, random
P = {"hbf": (50.7677, 6.0912), "elisen": (50.7745, 6.0855), "superc": (50.7780, 6.0780),
     "eschweiler": (50.8165, 6.2725), "stolberg": (50.7700, 6.2270), "herzogenrath": (50.8700, 6.0940),
     "monschau": (50.5550, 6.2400), "uniklinik": (50.7760, 6.0450)}
PAIRS = [("hbf", "elisen"), ("hbf", "superc"), ("hbf", "eschweiler"), ("superc", "uniklinik"),
         ("hbf", "herzogenrath"), ("stolberg", "monschau"), ("elisen", "uniklinik")]
def post(url, body):
    t = time.perf_counter(); r = urllib.request.urlopen(urllib.request.Request(url, json.dumps(body).encode(), {"Content-Type": "application/json"}), timeout=30).read(); return (time.perf_counter() - t) * 1000, r
def get(url):
    t = time.perf_counter(); r = urllib.request.urlopen(url, timeout=30).read(); return (time.perf_counter() - t) * 1000, r
def rss(pid):
    for l in open(f"/proc/{pid}/status"):
        if l.startswith(("VmRSS", "VmHWM")): print("  ", l.strip())
def summary(name, xs):
    xs = sorted(xs); print(f"{name}: n={len(xs)} median {statistics.median(xs):.1f} ms, p95 {xs[int(len(xs)*0.95)-1]:.1f} ms, max {xs[-1]:.1f} ms")
mode = sys.argv[1]
if mode == "valhalla":
    U = "http://127.0.0.1:8002"
    for costing in ("pedestrian", "bicycle", "auto"):
        ts = []
        for _ in range(3):
            for a, b in PAIRS:
                ms, r = post(U + "/route", {"locations": [{"lat": P[a][0], "lon": P[a][1]}, {"lat": P[b][0], "lon": P[b][1]}], "costing": costing, "language": "de-DE"})
                ts.append(ms)
        summary("route " + costing, ts)
    random.seed(1); ts = []
    for a in P:
        tg = [{"lat": P[a][0] + random.uniform(-0.009, 0.009), "lon": P[a][1] + random.uniform(-0.014, 0.014)} for _ in range(30)]
        for _ in range(3):
            ms, r = post(U + "/sources_to_targets", {"sources": [{"lat": P[a][0], "lon": P[a][1]}], "targets": tg, "costing": "pedestrian"}); ts.append(ms)
    summary("matrix 1x30 pedestrian (1 km)", ts)
elif mode == "access":
    # Erste/letzte Meile: Matrix 1×N auf die echten Zugangshalte (raptorprobe -access-out, 800 m Luftlinie).
    U = "http://127.0.0.1:8002"
    A = json.load(open(sys.argv[2] if len(sys.argv) > 2 else "/w/access.json"))
    def grid(ss):  # Bahnsteige im 25-m-Raster zusammenfassen
        return list({(round(x["Lat"] / 0.000225), round(x["Lon"] / 0.000355)): x for x in ss}.values())
    for name, verbose, red in (("alle Halte", True, list), ("verbose=false", False, list), ("25-m-Raster", False, grid)):
        ts = []; ns = []
        for k, v in A.items():
            ss = red(v["stops"] or [])
            if k == "koeln" or not ss: continue  # außerhalb der Tiles
            ns.append(len(ss))
            body = {"sources": [{"lat": v["lat"], "lon": v["lon"]}], "targets": [{"lat": x["Lat"], "lon": x["Lon"]} for x in ss],
                    "costing": "pedestrian", "costing_options": {"pedestrian": {"walking_speed": 4.3}}, "verbose": verbose}
            for _ in range(5):
                ms, r = post(U + "/sources_to_targets", body); ts.append(ms)
        summary(f"access {name} (Ziele {min(ns)}–{max(ns)})", ts)
elif mode == "motis-stops":
    import csv, io, zipfile
    U = "http://127.0.0.1:8080"
    z = zipfile.ZipFile("/w/raw/sr_gtfs.zip"); st = list(csv.DictReader(io.TextIOWrapper(z.open("stops.txt"), "utf-8-sig")))
    def near(p):
        return min((s for s in st if not s.get("parent_station")), key=lambda s: (float(s["stop_lat"]) - p[0]) ** 2 + (float(s["stop_lon"]) - p[1]) ** 2)["stop_id"]
    ids = {k: near(v) for k, v in P.items()}
    ts = []; ok = 0
    for rep in range(3):
        for a, b in PAIRS:
            q = f"{U}/api/v1/plan?fromPlace=srgtfs_{urllib.parse.quote(ids[a])}&toPlace=srgtfs_{urllib.parse.quote(ids[b])}&time=2026-10-28T07:30:00%2B01:00&numItineraries=3"
            ms, r = get(q); ts.append(ms); d = json.loads(r); ok += bool(d.get("itineraries"))
            if rep == 0: print(" ", a, b, len(d.get("itineraries", [])), "Verb.", [round(i["duration"]/60) for i in d.get("itineraries", [])], "min")
    summary("plan Halt-zu-Halt", ts); print("  mit Ergebnis:", ok, "/", len(ts))
else:
    U = "http://127.0.0.1:8080"
    ts = []; ok = 0
    for rep in range(3):
        for a, b in PAIRS:
            q = f"{U}/api/v1/plan?fromPlace={P[a][0]},{P[a][1]}&toPlace={P[b][0]},{P[b][1]}&time=2026-10-28T07:30:00%2B01:00&numItineraries=3"
            ms, r = get(q); ts.append(ms); d = json.loads(r); ok += bool(d.get("itineraries"))
            if rep == 0: print(" ", a, b, len(d.get("itineraries", [])), "Verb.", [round(i["duration"]/60) for i in d.get("itineraries", [])], "min")
    summary("plan ÖPNV+Fuß", ts); print("  mit Ergebnis:", ok, "/", len(ts))
