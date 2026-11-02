// Abweichen von der Route (D-36): rein auf dem Handy, der Server sieht nur die neue Routenanfrage.
import 'dart:math';

import 'package:latlong2/latlong.dart';

const schwelleAuto = 60.0, schwelleSonst = 40.0; // Meter neben dem Weg, dazu die GPS-Genauigkeit
const _maxGenau = 50.0; // ungenauer sagt der Fix nichts (Tunnel, Häuserschlucht)
const _fixeDaneben = 3; // ein Ausreißer allein löst nichts aus

/// Abstand in Metern vom Punkt zum Linienzug (flach gerechnet, reicht für die paar Meter, auf die es ankommt).
double abstandZumWeg(double lat, double lon, List<LatLng> weg) {
  final mLat = 111320.0, mLon = 111320.0 * cos(lat * pi / 180);
  double x(LatLng p) => (p.longitude - lon) * mLon;
  double y(LatLng p) => (p.latitude - lat) * mLat;
  var best = double.infinity;
  for (var i = 0; i < weg.length; i++) {
    final (ax, ay) = (x(weg[i]), y(weg[i]));
    var d = ax * ax + ay * ay;
    if (i + 1 < weg.length) {
      final (dx, dy) = (x(weg[i + 1]) - ax, y(weg[i + 1]) - ay);
      final l = dx * dx + dy * dy;
      final t = l == 0 ? 0.0 : (-(ax * dx + ay * dy) / l).clamp(0.0, 1.0);
      final (px, py) = (ax + t * dx, ay + t * dy);
      d = px * px + py * py;
    }
    if (d < best) best = d;
  }
  return sqrt(best);
}

/// Merkt sich, ob die letzten GPS-Positionen alle neben [weg] lagen.
class Abweichung {
  final List<LatLng> weg;
  final double schwelle;
  int _daneben = 0;
  Abweichung(this.weg, this.schwelle);

  /// true: wirklich abgewichen, die Route sollte neu berechnet werden.
  bool pruefen(double lat, double lon, double genau) {
    if (genau > _maxGenau) return false;
    if (abstandZumWeg(lat, lon, weg) <= schwelle + genau) {
      _daneben = 0;
      return false;
    }
    return ++_daneben >= _fixeDaneben;
  }
}
