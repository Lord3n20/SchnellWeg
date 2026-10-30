// Client für routing-core (server/internal/api). Koordinaten gehen nur im Request-Body von /v1/route raus;
// Abfahrten fragt die App nur mit Halte-IDs ab (app-ui.md, Datenschutz).
import 'dart:convert';
import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import 'standort.dart';
import 'zugang.dart';

class ApiFehler implements Exception {
  final String art; // Fehlertyp vom Server ("fahrplan_laedt" …) oder "netz"
  final Map? daten; // ganze Fehlerantwort (z. B. "seite", bei fussweg_lang die Schätzung "start"/"ziel")
  ApiFehler(this.art, [this.daten]);
  String? get seite => daten?['seite'] as String?;
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

/// Suchtreffer: Adresse/Ort vom Server oder Haltestelle aus der lokalen SQLite.
class Ort {
  final String art; // adresse | strasse | ort | poi | halt | gebaeude | karte
  final String name, detail;
  final double lat, lon;
  Ort(this.art, this.name, this.detail, this.lat, this.lon);

  Ort.json(Map<String, dynamic> j)
      : art = j['kind'] as String,
        name = j['name'] as String,
        detail = (j['detail'] as String?) ?? '',
        lat = (j['lat'] as num).toDouble(),
        lon = (j['lon'] as num).toDouble();
}

class KartenRegion {
  final String id, name;
  final double west, sued, ost, nord;
  final int zoom, bytes;
  final DateTime stand;

  KartenRegion.json(Map<String, dynamic> j)
      : id = j['id'] as String,
        name = j['name'] as String,
        west = _box(j, 0),
        sued = _box(j, 1),
        ost = _box(j, 2),
        nord = _box(j, 3),
        zoom = j['zoom'] as int,
        bytes = j['bytes'] as int,
        stand = DateTime.parse(j['stand'] as String);

  Map<String, dynamic> toJson() => {
        'id': id, 'name': name, 'box': '$west,$sued,$ost,$nord', 'zoom': zoom, 'bytes': bytes,
        'stand': stand.toUtc().toIso8601String(),
      };

  static double _box(Map<String, dynamic> j, int i) => double.parse((j['box'] as String).split(',')[i]);

  bool enthaelt(double lat, double lon) => lat >= sued && lat <= nord && lon >= west && lon <= ost;
}

class HaltRef {
  final String id, name;
  HaltRef.json(Map<String, dynamic> j)
      : id = j['id'] as String,
        name = j['name'] as String;
}

class Abschnitt {
  final bool fahrt; // sonst Fußweg
  final HaltRef? von, nach; // null = Start- bzw. Zielort
  final DateTime ab, an;
  final DateTime? abRt, anRt;
  final String linie, ziel;
  final bool fern, risiko, faelltAus, entfaellt;
  final bool rad; // Fahrrad fährt mit → Fahrradticket nötig (D-21)
  final String form; // Fußweg: Wegform (encoded polyline, Genauigkeit 6), "" = unbekannt
  final List<String> halte; // Fahrt: Halte-IDs vom Ein- bis zum Ausstieg

  Abschnitt.json(Map<String, dynamic> j)
      : fahrt = j['mode'] == 'ride',
        von = j['from'] == null ? null : HaltRef.json(j['from'] as Map<String, dynamic>),
        nach = j['to'] == null ? null : HaltRef.json(j['to'] as Map<String, dynamic>),
        ab = _zeit(j['dep'])!,
        an = _zeit(j['arr'])!,
        abRt = _zeit(j['dep_rt']),
        anRt = _zeit(j['arr_rt']),
        linie = (j['line'] as String?) ?? '',
        ziel = (j['headsign'] as String?) ?? '',
        fern = j['fern'] == true,
        rad = j['rad'] == true,
        risiko = j['risk'] == true,
        faelltAus = j['canceled'] == true,
        entfaellt = j['skipped'] == true,
        form = (j['shape'] as String?) ?? '',
        halte = (j['stops'] as List?)?.cast<String>() ?? const [];

  /// Verspätung der Abfahrt in ganzen Minuten, null ohne Echtzeit.
  int? get versp => abRt == null ? null : (abRt!.difference(ab).inSeconds / 60).round();
}

class Verbindung {
  final int fahrten;
  final DateTime ab, an;
  final DateTime? anRt;
  final String risiko; // "" | anschluss | ausfall
  final double? eilKmh; // nur mit diesem Tempo zum ersten Halt zu schaffen (D-31), null = normal erreichbar
  final List<Abschnitt> abschnitte;

  Verbindung.json(Map<String, dynamic> j)
      : fahrten = j['trips'] as int,
        ab = _zeit(j['dep'])!,
        an = _zeit(j['arr'])!,
        anRt = _zeit(j['arr_rt']),
        risiko = (j['risk'] as String?) ?? '',
        eilKmh = (j['eil_kmh'] as num?)?.toDouble(),
        abschnitte = [for (final l in (j['legs'] as List).cast<Map<String, dynamic>>()) Abschnitt.json(l)];

  DateTime get ankunft => anRt ?? an;
}

/// Dekodiert eine encoded polyline (Valhalla: Genauigkeit 6) zu (lat, lon)-Paaren; kaputte Eingaben → leer.
List<(double, double)> polyline(String s, {int stellen = 6}) {
  final teiler = stellen == 6 ? 1e6 : 1e5;
  final out = <(double, double)>[];
  var i = 0, lat = 0, lon = 0;
  int? zahl() {
    var wert = 0, shift = 0;
    while (i < s.length) {
      final b = s.codeUnitAt(i++) - 63;
      wert |= (b & 0x1f) << shift;
      shift += 5;
      if (b < 0x20) return (wert & 1) != 0 ? ~(wert >> 1) : wert >> 1;
    }
    return null;
  }

  while (i < s.length) {
    final dLat = zahl(), dLon = zahl();
    if (dLat == null || dLon == null) return const [];
    lat += dLat;
    lon += dLon;
    out.add((lat / teiler, lon / teiler));
  }
  return out;
}

// Oberste Ebene, damit der Abschluss nichts von Api (HttpClient) ins Isolate mitnimmt.
Future<List<Halt>> _halteImIsolate(Uint8List daten) => Isolate.run(() => _halteLesen(daten));

List<Halt> _halteLesen(Uint8List daten) {
  final rows = (utf8.decoder.fuse(json.decoder).convert(daten) as Map)['stops'] as List;
  return [for (final r in rows.cast<List>()) Halt(r[0] as String, r[1] as String, (r[2] as num).toDouble(), (r[3] as num).toDouble())];
}

DateTime? _zeit(Object? v) => v == null ? null : DateTime.parse(v as String).toLocal();

class Api {
  final Zugang zugang;
  final HttpClient _http;
  Api(this.zugang) : _http = zugang.client();

  Future<(int, String?, Uint8List)> _anfrage(String methode, String pfad,
      {Object? body, Map<String, String> kopf = const {}, Duration zeit = const Duration(seconds: 8)}) async {
    try {
      final req = await _http.openUrl(methode, zugang.basis.resolve(pfad));
      kopf.forEach(req.headers.set);
      if (body != null) {
        req.headers.contentType = ContentType.json;
        req.write(jsonEncode(body));
      }
      final res = await req.close().timeout(zeit);
      // BytesBuilder statt List<int>: die Haltestellenliste (~6 MB) wäre sonst ein Vielfaches davon groß und langsam
      final daten = (await res.fold(BytesBuilder(copy: false), (b, d) => b..add(d)).timeout(zeit)).takeBytes();
      if (res.statusCode >= 400) {
        String art = 'http_${res.statusCode}';
        Map? m;
        try {
          m = jsonDecode(utf8.decode(daten)) as Map;
          art = m['error'] as String? ?? art;
        } catch (_) {}
        throw ApiFehler(art, m);
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
    // ~110 000 Halte: Dekodieren in einem eigenen Isolate, sonst stockt die Oberfläche beim täglichen Abgleich
    return (neu, await _halteImIsolate(daten));
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

  /// Adressen und Orte; der Suchtext geht nur im Body raus. [nahe]: ungefährer Standort, damit nahe Treffer
  /// vorn stehen – auf 0,05° (~4 km) gerundet, genauer muss der Server es nicht wissen.
  Future<List<Ort>> suche(String q, {int n = 10, Position? nahe}) async {
    double grob(double x) => (x * 20).round() / 20;
    final (_, _, daten) = await _anfrage('POST', '/v1/search', body: {
      'q': q,
      'n': n,
      if (nahe != null) 'near': [grob(nahe.lat), grob(nahe.lon)],
    });
    final hits = (jsonDecode(utf8.decode(daten)) as Map)['hits'] as List? ?? const [];
    return [for (final h in hits.cast<Map<String, dynamic>>()) Ort.json(h)];
  }

  /// Verbindungen (mehrere Abfahrten, ggf. knappe vorn) und ob der Echtzeitstand verfügbar war.
  Future<(List<Verbindung>, bool)> route(double vonLat, double vonLon, double nachLat, double nachLon,
      {DateTime? ab, bool ankunft = false, double? kmh, String? art, bool fern = false, bool lang = false, bool knapp = false}) async {
    final (_, _, daten) = await _anfrage('POST', '/v1/route',
        body: {
          'from': {'lat': vonLat, 'lon': vonLon},
          'to': {'lat': nachLat, 'lon': nachLon},
          if (ab != null) 'time': ab.toUtc().toIso8601String(),
          if (ab != null && ankunft) 'ankunft': true, // ab ist dann die späteste Ankunft (D-27)
          if (fern) 'fern': true,
          'kmh': ?kmh,
          'art': ?art, // fuss | einrad | klapprad | fixie | rennrad | mtb (D-21, D-22)
          if (lang) 'lang': true, // auch mit Fußweg > 5 km genau rechnen (D-25)
          if (knapp && !ankunft) 'knapp': true, // auch Verbindungen, die nur mit schnellerem Zugangsweg klappen (D-31)
        },
        zeit: const Duration(seconds: 15));
    final j = jsonDecode(utf8.decode(daten)) as Map<String, dynamic>;
    return (
      [for (final v in (j['journeys'] as List? ?? const []).cast<Map<String, dynamic>>()) Verbindung.json(v)],
      j['rt'] == true
    );
  }

  /// Kartendateien, die der Server anbietet (D-20): Übersicht + Bundesländer.
  Future<List<KartenRegion>> kartenRegionen() async {
    final (_, _, daten) = await _anfrage('GET', '/v1/map');
    final l = (jsonDecode(utf8.decode(daten)) as Map)['regions'] as List? ?? const [];
    return [for (final r in l.cast<Map<String, dynamic>>()) KartenRegion.json(r)];
  }

  /// Lädt die Kartendatei [id] nach [ziel], wenn sie neuer ist als [stand]. Geschrieben wird in eine .part-Datei,
  /// die bei Abbruch liegen bleibt und beim nächsten Versuch per Range fortgesetzt wird (If-Range: nur wenn der
  /// Serverstand gleich geblieben ist); erst die fertige Datei wird umbenannt. true = neue Datei.
  Future<bool> karte(String id, File ziel, DateTime? stand, {void Function(int geladen, int gesamt)? fortschritt}) async {
    final teil = File('${ziel.path}.part');
    final teilStand = File('${ziel.path}.part.stand'); // Last-Modified des angefangenen Downloads
    try {
      var schon = await teil.exists() && await teilStand.exists() ? await teil.length() : 0;
      final req = await _http.getUrl(zugang.basis.resolve('/v1/map/${Uri.encodeComponent(id)}'));
      if (stand != null) req.headers.ifModifiedSince = stand;
      if (schon > 0) {
        req.headers.set('Range', 'bytes=$schon-');
        req.headers.set('If-Range', (await teilStand.readAsString()).trim());
      }
      final res = await req.close().timeout(const Duration(seconds: 15));
      if (res.statusCode == 304) {
        await res.drain<void>();
        return false;
      }
      if (res.statusCode == 200) {
        schon = 0; // neu oder Serverstand geändert
      } else if (res.statusCode != 206) {
        await res.drain<void>();
        throw ApiFehler('http_${res.statusCode}');
      }
      final lm = res.headers.value('last-modified') ?? HttpDate.format(DateTime.now());
      await teilStand.writeAsString(lm);
      final gesamt = res.contentLength < 0 ? -1 : schon + res.contentLength;
      final aus = teil.openWrite(mode: schon > 0 ? FileMode.append : FileMode.write);
      var n = schon;
      try {
        await for (final b in res.timeout(const Duration(seconds: 30))) {
          aus.add(b);
          n += b.length;
          fortschritt?.call(n, gesamt);
        }
      } finally {
        await aus.close();
      }
      if (gesamt >= 0 && n != gesamt) throw ApiFehler('karte_unvollstaendig');
      await teil.setLastModified(HttpDate.parse(lm));
      await teil.rename(ziel.path);
      await teilStand.delete();
      return true;
    } on ApiFehler {
      rethrow;
    } on Exception catch (e) {
      // .part bleibt für den nächsten Versuch
      throw ApiFehler(e is HandshakeException || e is TlsException ? 'zertifikat' : 'netz');
    }
  }

  Future<Map<String, dynamic>> status() async {
    final (_, _, daten) = await _anfrage('GET', '/v1/status');
    return jsonDecode(utf8.decode(daten)) as Map<String, dynamic>;
  }

  void schliessen() => _http.close(force: true);
}
