// Auswertung des Messgangs (D-44): Der eigene Server ordnet die Strecke Steigung und Belag zu, hier entstehen daraus die Einstellungen.
import 'dart:async';
import 'dart:math';

import 'api.dart';
import 'tempo.dart';
import 'zugang.dart';

const _ebenBis = 1.5; // % gilt noch als eben
const _belagEbenBis = 2.5; // % Steigung, bis zu der ein Belag gewertet wird
const _ebenMinM = 150.0, _hangMinM = 100.0, _zuFussMinM = 50.0, _belagMinM = 100.0; // so viel Strecke braucht ein Wert
const _reinGehenKmh = standardKmh * 1.3; // bis zu diesem Tempo wird gelaufen
const _rolltAbKmh = 7.0; // langsamer ist es kein rollendes Profil

class SpurPunkt {
  final double lat, lon, m, s; // m und s seit dem vorigen Punkt
  const SpurPunkt(this.lat, this.lon, this.m, this.s);
}

class Stueck {
  final double g, m, s; // Steigung in %, Meter, Sekunden
  final int c; // Belagklasse, 0 = unauffällig
  const Stueck(this.g, this.m, this.s, [this.c = 0]);
}

/// Was der Messgang ergeben hat; null = zu wenig Strecke dafür, die Einstellung bleibt.
class HangErgebnis {
  final double kmhEben;
  final int? auf, ab, zuFussAb;
  final Map<int, Object?> belag; // Klasse → Regel, null = wie normal
  const HangErgebnis(this.kmhEben, this.auf, this.ab, this.zuFussAb, [this.belag = const {}]);
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
/// [fuss]: Schlüssel des Fuß-Profils, das auf Belägen gilt, auf denen gelaufen wird.
HangErgebnis hangAuswerten(List<Stueck> alle, double kmh, {required bool eigen, String? fuss}) {
  final st = alle.where((x) => x.c == 0).toList(); // schlechter Belag soll Ebene und Hang nicht verfälschen
  final (fm, fs, _) = _summe(st.where((x) => x.g.abs() < _ebenBis));
  final eben = fm >= _ebenMinM && fs > 0 ? fm / fs * 3.6 : kmh;
  final hoch = st.where((x) => x.g >= _ebenBis).toList();
  final runter = st.where((x) => x.g <= -_ebenBis);
  final rollt = eigen && eben >= _rolltAbKmh;
  int? zuFuss;
  if (rollt && _summe(hoch).$1 >= _hangMinM) {
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
  final belag = <int, Object?>{};
  for (var c = 1; c <= 5; c++) {
    final (m, s, _) = _summe(alle.where((x) => x.c == c && x.g.abs() < _belagEbenBis));
    if (m < _belagMinM || s <= 0) continue;
    final v = m / s * 3.6;
    if (rollt && fuss != null && v <= _reinGehenKmh) {
      belag[c] = fuss;
    } else {
      final p = ((v / eben - 1) * 10).round() * 10;
      belag[c] = p >= 0 ? null : p.clamp(-90, -10);
    }
  }
  return HangErgebnis(
    (eben * 10).round() / 10,
    zuFuss != null && zuFuss > 0 ? 0 : _prozent(hoch, eben, aufBereich),
    _prozent(runter, eben, abBereich),
    zuFuss,
    belag,
  );
}

double _rund(double x, int stellen) {
  final f = pow(10, stellen);
  return (x * f).round() / f;
}

/// Ganze Auswertung; null, wenn der Server sie nicht liefern kann (dann zählt nur das gemessene Tempo).
Future<HangErgebnis?> hangMessen(List<SpurPunkt> spur, double kmh, {required bool eigen, String? fuss}) async {
  Api? api;
  try {
    final Zugang? z = await Zugang.laden();
    if (z == null || spur.length < 2) return null;
    api = Api(z);
    final antwort = await api.messung([
      for (final p in spur) [_rund(p.lat, 6), _rund(p.lon, 6), _rund(p.m, 1), _rund(p.s, 1)]
    ]);
    final st = [
      for (final x in antwort) Stueck((x['g'] as num).toDouble(), (x['m'] as num).toDouble(), (x['s'] as num).toDouble(), (x['c'] as num).toInt())
    ];
    return st.isEmpty ? null : hangAuswerten(st, kmh, eigen: eigen, fuss: fuss);
  } on Exception {
    return null;
  } on TypeError {
    return null;
  } finally {
    api?.schliessen();
  }
}
