// Steigung aus dem Messgang (D-44): Die Spur bleibt auf dem Handy. Vom Server kommt nur die Höhenkarte der 1°-Kachel,
// einmal geladen und im App-Ordner aufgehoben. Daraus entsteht die Steigung je Abschnitt und aus deren Tempo die Regler.
import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'zugang.dart';
import 'tempo.dart';

const hangAbschnittM = 50.0; // kürzer rauscht die Steigung zu sehr
const _ebenBis = 1.5; // % gilt noch als eben
const _ebenMinM = 150.0, _hangMinM = 100.0, _zuFussMinM = 50.0; // so viel Strecke braucht ein Wert
const _reinGehenKmh = standardKmh * 1.3; // bis zu diesem Tempo wird bergauf gelaufen
const _rolltAbKmh = 7.0; // langsamer ist es kein rollendes Profil
const _hgtSeite = 3601;

class SpurPunkt {
  final double lat, lon, m, s; // m und s seit dem vorigen Punkt
  const SpurPunkt(this.lat, this.lon, this.m, this.s);
}

class Stueck {
  final double g, m, s; // Steigung in %, Meter, Sekunden
  const Stueck(this.g, this.m, this.s);
  double get kmh => m / s * 3.6;
}

/// Was der Messgang ergeben hat; null = zu wenig Strecke dafür, der Regler bleibt.
class HangErgebnis {
  final double kmhEben;
  final int? auf, ab, zuFussAb;
  const HangErgebnis(this.kmhEben, this.auf, this.ab, this.zuFussAb);
}

/// Name der 1°-Kachel, z. B. N50E006.
String kachelName(double lat, double lon) {
  final la = lat.floor(), lo = lon.floor();
  return '${la < 0 ? 'S' : 'N'}${la.abs().toString().padLeft(2, '0')}${lo < 0 ? 'W' : 'E'}${lo.abs().toString().padLeft(3, '0')}';
}

class HoehenKachel {
  final ByteData _d;
  final int _n; // Punkte je Seite
  final double _lat0, _lon0; // Südwestecke
  HoehenKachel(Uint8List b, this._lat0, this._lon0, {int seite = _hgtSeite})
      : _d = ByteData.sublistView(b),
        _n = seite;

  double? _punkt(int zeile, int spalte) {
    final v = _d.getInt16(2 * (zeile * _n + spalte));
    return v == -32768 ? null : v.toDouble();
  }

  /// Höhe in m, zwischen den vier Nachbarpunkten gemittelt; null ohne Daten.
  double? hoehe(double lat, double lon) {
    final y = (_lat0 + 1 - lat) * (_n - 1), x = (lon - _lon0) * (_n - 1);
    final z = min(y.floor(), _n - 2), s = min(x.floor(), _n - 2);
    if (z < 0 || s < 0) return null;
    final a = _punkt(z, s), b = _punkt(z, s + 1), c = _punkt(z + 1, s), d = _punkt(z + 1, s + 1);
    if (a == null || b == null || c == null || d == null) return null;
    final dy = y - z, dx = x - s;
    return a * (1 - dx) * (1 - dy) + b * dx * (1 - dy) + c * (1 - dx) * dy + d * dx * dy;
  }
}

/// Teilt die Spur in Abschnitte von mindestens [hangAbschnittM] mit ihrer Steigung.
List<Stueck> stuecke(List<SpurPunkt> spur, double? Function(double, double) hoehe) {
  final out = <Stueck>[];
  double? h0;
  var m = 0.0, s = 0.0;
  for (final p in spur) {
    final h = hoehe(p.lat, p.lon);
    if (h == null) {
      h0 = null;
      m = s = 0;
      continue;
    }
    if (h0 == null) {
      h0 = h;
      continue;
    }
    m += p.m;
    s += p.s;
    if (m >= hangAbschnittM && s > 0) {
      out.add(Stueck((h - h0) / m * 100, m, s));
      h0 = h;
      m = s = 0;
    }
  }
  return out;
}

(double, double, double) _summe(Iterable<Stueck> l) {
  var m = 0.0, s = 0.0, g = 0.0;
  for (final x in l) {
    m += x.m;
    s += x.s;
    g += x.g.abs() * x.m;
  }
  return (m, s, m > 0 ? g / m : 0);
}

int? _prozent(Iterable<Stueck> l, double eben, (int, int) b) {
  final (m, s, g) = _summe(l);
  if (m < _hangMinM || s <= 0 || g <= 0) return null;
  final v = m / s * 3.6; // gemessen bei Steigung g; der Regler meint das Tempo bei voller Steigung
  final voll = eben + (v - eben) * 6 / min(g, 6);
  return (((voll / eben - 1) * 100 / hangSchritt).round() * hangSchritt).clamp(b.$1, b.$2);
}

/// Wertet die Abschnitte aus. [kmh]: Schnitt des Messgangs, gilt als eben, wenn es zu wenig Ebene gab.
HangErgebnis hangAuswerten(List<Stueck> st, double kmh, {required bool eigen}) {
  final flach = st.where((x) => x.g.abs() < _ebenBis);
  final (fm, fs, _) = _summe(flach);
  final eben = fm >= _ebenMinM && fs > 0 ? fm / fs * 3.6 : kmh;
  final hoch = st.where((x) => x.g >= _ebenBis).toList();
  final runter = st.where((x) => x.g <= -_ebenBis);
  int? zuFuss;
  if (eigen && eben >= _rolltAbKmh && _summe(hoch).$1 >= _hangMinM) {
    zuFuss = 0;
    for (var g = 2; g <= zuFussMax; g++) {
      final (m, s, _) = _summe(hoch.where((x) => x.g >= g && x.g < g + 1)); // genau diese Steigung …
      final (mDrueber, sDrueber, _) = _summe(hoch.where((x) => x.g >= g)); // … und alles Steilere ebenfalls
      if (m >= _zuFussMinM && m / s * 3.6 <= _reinGehenKmh && mDrueber / sDrueber * 3.6 <= _reinGehenKmh) {
        zuFuss = g;
        break;
      }
    }
  }
  return HangErgebnis(
    (eben * 10).round() / 10,
    zuFuss != null && zuFuss > 0 ? 0 : _prozent(hoch, eben, aufBereich),
    _prozent(runter, eben, abBereich),
    zuFuss,
  );
}

Future<HoehenKachel?> _kachel(Api api, Directory ordner, String name) async {
  final datei = File('${ordner.path}/$name.hgt');
  final b = await datei.exists() ? await datei.readAsBytes() : await api.hoehenKachel(name);
  if (b == null || b.length != _hgtSeite * _hgtSeite * 2) return null;
  if (!await datei.exists()) {
    final zw = File('${datei.path}.neu');
    await zw.writeAsBytes(b, flush: true);
    await zw.rename(datei.path);
  }
  final la = int.parse(name.substring(1, 3)) * (name[0] == 'S' ? -1 : 1);
  final lo = int.parse(name.substring(4)) * (name[3] == 'W' ? -1 : 1);
  return HoehenKachel(b, la.toDouble(), lo.toDouble());
}

/// Ganze Auswertung; null, wenn Höhen oder Zugang fehlen (dann zählt nur das gemessene Tempo).
Future<HangErgebnis?> hangMessen(List<SpurPunkt> spur, double kmh, {required bool eigen}) async {
  Api? api;
  try {
    final Zugang? z = await Zugang.laden();
    if (z == null || spur.isEmpty) return null;
    api = Api(z);
    final ordner = await Directory('${(await getApplicationSupportDirectory()).path}/hoehe').create(recursive: true);
    final kacheln = <String, HoehenKachel?>{};
    for (final n in {for (final p in spur) kachelName(p.lat, p.lon)}) {
      kacheln[n] = await _kachel(api, ordner, n);
    }
    final st = stuecke(spur, (la, lo) => kacheln[kachelName(la, lo)]?.hoehe(la, lo));
    return st.isEmpty ? null : hangAuswerten(st, kmh, eigen: eigen);
  } on Exception {
    return null;
  } finally {
    api?.schliessen();
  }
}
