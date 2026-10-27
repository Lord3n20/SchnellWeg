// Client für routing-core (server/internal/api). Koordinaten gehen nur im Request-Body von /v1/route raus;
// Abfahrten fragt die App nur mit Halte-IDs ab (app-ui.md, Datenschutz).
import 'dart:convert';
import 'dart:io';

import 'zugang.dart';

class ApiFehler implements Exception {
  final String art; // Fehlertyp vom Server ("fahrplan_laedt" …) oder "netz"
  ApiFehler(this.art);
  @override
  String toString() => art;
}

class Halt {
  final String id, name;
  final double lat, lon;
  Halt(this.id, this.name, this.lat, this.lon);
}

class Abfahrt {
  final String line, headsign, tripId;
  final DateTime dep;
  final DateTime? depRt;
  final bool canceled, fern;

  Abfahrt.json(Map<String, dynamic> j)
      : line = j['line'] as String,
        headsign = (j['headsign'] as String?) ?? '',
        tripId = j['trip_id'] as String,
        dep = DateTime.parse(j['dep'] as String).toLocal(),
        depRt = j['dep_rt'] == null ? null : DateTime.parse(j['dep_rt'] as String).toLocal(),
        canceled = j['canceled'] == true,
        fern = j['fern'] == true;

  DateTime get erwartet => depRt ?? dep;

  /// Verspätung in ganzen Minuten, null ohne Echtzeit.
  int? get versp => depRt == null ? null : (depRt!.difference(dep).inSeconds / 60).round();
}

class Api {
  final Zugang zugang;
  final HttpClient _http;
  Api(this.zugang) : _http = zugang.client();

  Future<(int, String?, List<int>)> _anfrage(String methode, String pfad,
      {Object? body, Map<String, String> kopf = const {}, Duration zeit = const Duration(seconds: 8)}) async {
    try {
      final req = await _http.openUrl(methode, zugang.basis.resolve(pfad));
      kopf.forEach(req.headers.set);
      if (body != null) {
        req.headers.contentType = ContentType.json;
        req.write(jsonEncode(body));
      }
      final res = await req.close().timeout(zeit);
      final daten = await res.fold<List<int>>(<int>[], (a, b) => a..addAll(b)).timeout(zeit);
      if (res.statusCode >= 400) {
        String art = 'http_${res.statusCode}';
        try {
          art = (jsonDecode(utf8.decode(daten)) as Map)['error'] as String? ?? art;
        } catch (_) {}
        throw ApiFehler(art);
      }
      return (res.statusCode, res.headers.value('etag'), daten);
    } on ApiFehler {
      rethrow;
    } on Exception catch (e) {
      throw ApiFehler(e is HandshakeException || e is TlsException ? 'zertifikat' : 'netz');
    }
  }

  /// Alle Halte; null, wenn sich seit [etag] nichts geändert hat.
  Future<(String?, List<Halt>)?> halte(String? etag) async {
    final (code, neu, daten) = await _anfrage('GET', '/v1/stops',
        kopf: etag == null ? const {} : {'If-None-Match': etag}, zeit: const Duration(seconds: 30));
    if (code == 304) return null;
    final rows = (jsonDecode(utf8.decode(daten)) as Map)['stops'] as List;
    return (
      neu,
      [for (final r in rows.cast<List>()) Halt(r[0] as String, r[1] as String, (r[2] as num).toDouble(), (r[3] as num).toDouble())]
    );
  }

  /// Nächste Abfahrten je Halt-ID (höchstens 30 IDs) und ob der Echtzeitstand verfügbar war.
  Future<(Map<String, List<Abfahrt>>, bool)> abfahrten(List<String> ids, {int n = 3}) async {
    final (_, _, daten) = await _anfrage('POST', '/v1/departures', body: {'stops': ids, 'n': n});
    final j = jsonDecode(utf8.decode(daten)) as Map<String, dynamic>;
    final out = <String, List<Abfahrt>>{};
    (j['departures'] as Map<String, dynamic>).forEach((id, l) {
      out[id] = [for (final a in (l as List).cast<Map<String, dynamic>>()) Abfahrt.json(a)];
    });
    return (out, j['rt'] == true);
  }

  Future<Map<String, dynamic>> status() async {
    final (_, _, daten) = await _anfrage('GET', '/v1/status');
    return jsonDecode(utf8.decode(daten)) as Map<String, dynamic>;
  }

  void schliessen() => _http.close(force: true);
}
