#!/usr/bin/env python3
"""GTFS-Auszug: alle Fahrten, die mindestens einen Halt in der Box haben (vollständig, mit allen Halten).

Aufruf: gtfs_bbox.py <gtfs.zip> <minlon,minlat,maxlon,maxlat> <aus.zip>
Liest stop_times.txt zweimal streamend aus dem Zip (kein Entpacken, wenig RAM).
"""
import csv, io, sys, time, zipfile

src, box, dst = sys.argv[1], [float(x) for x in sys.argv[2].split(",")], sys.argv[3]
t0 = time.time()
z = zipfile.ZipFile(src)


def rows(name):
    with z.open(name) as f:
        r = csv.reader(io.TextIOWrapper(f, encoding="utf-8-sig", newline=""))
        yield next(r)
        yield from r


def col(h, *names):
    return [h.index(n) for n in names]


it = rows("stops.txt"); h = next(it); i_id, i_lat, i_lon = col(h, "stop_id", "stop_lat", "stop_lon")
inbox = set()
for r in it:
    try:
        if box[0] <= float(r[i_lon]) <= box[2] and box[1] <= float(r[i_lat]) <= box[3]:
            inbox.add(r[i_id])
    except ValueError:
        pass

it = rows("stop_times.txt"); h = next(it); i_trip, i_stop = col(h, "trip_id", "stop_id")
trips = {r[i_trip] for r in it if r[i_stop] in inbox}
print(f"Halte in Box {len(inbox)}, Fahrten {len(trips)} ({time.time()-t0:.0f} s)", file=sys.stderr)

out = zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED)


def write(name, keep):
    buf = io.StringIO(); w = csv.writer(buf, lineterminator="\n"); it = rows(name); h = next(it); w.writerow(h)
    n = 0
    with out.open(name, "w") as f:
        for r in it:
            if keep(h, r):
                w.writerow(r); n += 1
                if buf.tell() > 1 << 20:
                    f.write(buf.getvalue().encode()); buf.seek(0); buf.truncate()
        f.write(buf.getvalue().encode())
    return n


used_stops = set()
def st_keep(h, r, i=col(rows("stop_times.txt").__next__(), "trip_id", "stop_id")):
    if r[i[0]] in trips:
        used_stops.add(r[i[1]]); return True
    return False
print("stop_times", write("stop_times.txt", st_keep), file=sys.stderr)

routes, services = set(), set()
def tr_keep(h, r):
    if r[h.index("trip_id")] in trips:
        routes.add(r[h.index("route_id")]); services.add(r[h.index("service_id")]); return True
    return False
write("trips.txt", tr_keep)

parents = set()
it = rows("stops.txt"); h = next(it)
if "parent_station" in h:
    ip = h.index("parent_station")
    parents = {r[ip] for r in it if r[h.index("stop_id")] in used_stops and r[ip]}
keep_stops = used_stops | parents
write("stops.txt", lambda h, r: r[h.index("stop_id")] in keep_stops)
agencies = set()
def ro_keep(h, r):
    if r[h.index("route_id")] in routes:
        agencies.add(r[h.index("agency_id")]) if "agency_id" in h else None; return True
    return False
write("routes.txt", ro_keep)
write("agency.txt", lambda h, r: not agencies or "agency_id" not in h or r[h.index("agency_id")] in agencies)
write("calendar.txt", lambda h, r: r[h.index("service_id")] in services)
write("calendar_dates.txt", lambda h, r: r[h.index("service_id")] in services)
for n in ("feed_info.txt", "attributions.txt"):
    if n in z.namelist():
        out.writestr(n, z.read(n))
out.close()
print(f"fertig: {len(trips)} Fahrten, {len(keep_stops)} Halte, {len(routes)} Linien ({time.time()-t0:.0f} s)", file=sys.stderr)
