// Lokale Haltestellen-SQLite: „Halte in der Nähe“ rechnet das Handy selbst, der Standort verlässt es nie.
import 'dart:math';

import 'package:path/path.dart' as p;
import 'package:sqflite/sqflite.dart';

import 'api.dart';

class HalteDb {
  final Database _db;
  HalteDb._(this._db);

  static Future<HalteDb> oeffnen() async {
    final db = await openDatabase(p.join(await getDatabasesPath(), 'halte.db'), version: 1, onCreate: (db, _) async {
      await db.execute('CREATE TABLE halt (id TEXT PRIMARY KEY, name TEXT NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL)');
      await db.execute('CREATE INDEX halt_lat ON halt(lat)');
      await db.execute('CREATE TABLE meta (k TEXT PRIMARY KEY, v TEXT)');
    });
    return HalteDb._(db);
  }

  Future<String?> etag() async {
    final r = await _db.query('meta', where: 'k = ?', whereArgs: ['etag']);
    return r.isEmpty ? null : r.first['v'] as String?;
  }

  Future<int> anzahl() async => Sqflite.firstIntValue(await _db.rawQuery('SELECT count(*) FROM halt')) ?? 0;

  /// Ersetzt alle Halte in einer Transaktion.
  Future<void> ersetzen(String? etag, List<Halt> halte) => _db.transaction((tx) async {
        await tx.delete('halt');
        final b = tx.batch();
        for (final h in halte) {
          b.insert('halt', {'id': h.id, 'name': h.name, 'lat': h.lat, 'lon': h.lon});
        }
        b.insert('meta', {'k': 'etag', 'v': etag}, conflictAlgorithm: ConflictAlgorithm.replace);
        await b.commit(noResult: true);
      });

  /// Halte im Umkreis [radius] Meter, nach Entfernung sortiert.
  Future<List<(Halt, double)>> naechste(double lat, double lon, {double radius = 800}) async {
    final dLat = radius / 111320, dLon = radius / (111320 * cos(lat * pi / 180));
    final rows = await _db.query('halt',
        where: 'lat BETWEEN ? AND ? AND lon BETWEEN ? AND ?', whereArgs: [lat - dLat, lat + dLat, lon - dLon, lon + dLon]);
    final out = <(Halt, double)>[];
    for (final r in rows) {
      final h = Halt(r['id'] as String, r['name'] as String, r['lat'] as double, r['lon'] as double);
      final d = entfernung(lat, lon, h.lat, h.lon);
      if (d <= radius) out.add((h, d));
    }
    out.sort((a, b) => a.$2.compareTo(b.$2));
    return out;
  }
}

/// Luftlinie in Metern (Haversine).
double entfernung(double lat1, double lon1, double lat2, double lon2) {
  const r = 6371000.0;
  final dLat = (lat2 - lat1) * pi / 180, dLon = (lon2 - lon1) * pi / 180;
  final a = sin(dLat / 2) * sin(dLat / 2) + cos(lat1 * pi / 180) * cos(lat2 * pi / 180) * sin(dLon / 2) * sin(dLon / 2);
  return 2 * r * asin(sqrt(a));
}
