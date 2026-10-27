// RWTH-Gebäude (Nummer oder Name) live aus RWTH Maps – inoffizielle Schnittstelle, vom Betreiber gewollt (D-18).
// Raus geht nur der Suchtext, nie der Standort. Fällt sie aus, liefert /v1/search die Gebäude aus dem Server-Stand.
import 'dart:convert';
import 'dart:io';

import 'api.dart';

const _basis = 'https://maps.rwth-aachen.de/navigator/libraries/rwth/ajax/getBuildings.asp?lang=de&q=';
const _zeit = Duration(seconds: 3);

final _http = HttpClient()
  ..connectionTimeout = _zeit
  ..userAgent = 'SchnellWeg';

final _frei = RegExp(r'[A-Za-z0-9@*_\-./]'); // wie escape(), nur „+“ kodiert (macht die Seite auch)

/// Wie JavaScripts escape(): ISO-8859-1-Bytes prozentkodiert (die Seite erwartet kein UTF-8).
String _latin1Url(String s) {
  final b = StringBuffer();
  for (final c in s.runes) {
    if (c > 0xff) continue;
    final ch = String.fromCharCode(c);
    if (_frei.hasMatch(ch)) {
      b.write(ch);
    } else {
      b.write('%${c.toRadixString(16).toUpperCase().padLeft(2, '0')}');
    }
  }
  return b.toString();
}

double? _zahl(Object? v) => v is num ? v.toDouble() : double.tryParse('${v ?? ''}');

/// Gebäude passend zu [q]; leere Liste bei jedem Fehler (die Server-Suche läuft ja parallel).
Future<List<Ort>> rwthSuche(String q) async {
  try {
    final req = await _http.getUrl(Uri.parse(_basis + _latin1Url(q.trim())));
    final res = await req.close().timeout(_zeit);
    final daten = await res.fold<List<int>>(<int>[], (a, b) => a..addAll(b)).timeout(_zeit);
    if (res.statusCode != 200) return [];
    final liste = jsonDecode(latin1.decode(daten)) as List;
    final out = <Ort>[];
    for (final g in liste.cast<Map<String, dynamic>>()) {
      final lat = _zahl(g['lat']), lon = _zahl(g['lng']);
      final name = (g['bez'] as String?)?.trim() ?? '';
      if (lat == null || lon == null || lat == 0 || name.isEmpty || (g['typ'] ?? 'b') != 'b') continue;
      final nr = g['gno'] as String? ?? '';
      out.add(Ort('gebaeude', nr.isEmpty || name.contains('[$nr]') ? name : '$name [$nr]',
          (g['adr'] as String?)?.trim() ?? '', lat, lon));
    }
    return out;
  } on Object {
    return [];
  }
}
