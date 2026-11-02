// Abweichen von der Route (D-36): rein auf dem Handy, der Server sieht nur die neue Routenanfrage.
import 'dart:math';

import 'package:latlong2/latlong.dart';

import 'api.dart';
import 'halte_db.dart';

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

/// Der nächste Abbiegehinweis von hier aus und wie weit es bis dahin ist, null ohne Hinweise oder hinter dem letzten.
(Schritt, double)? naechsterSchritt(List<LatLng> form, List<Schritt> schritte, double lat, double lon) {
  if (form.isEmpty || schritte.isEmpty) return null;
  var hier = 0;
  var best = double.infinity;
  for (var i = 0; i < form.length; i++) {
    final d = entfernung(lat, lon, form[i].latitude, form[i].longitude);
    if (d < best) (best, hier) = (d, i);
  }
  for (final s in schritte) {
    if (s.i <= hier || s.i >= form.length) continue;
    var meter = best;
    for (var i = hier; i < s.i; i++) {
      meter += entfernung(form[i].latitude, form[i].longitude, form[i + 1].latitude, form[i + 1].longitude);
    }
    return (s, meter);
  }
  return null;
}

/// „In 350 m“ / „In 2,4 km“, auf ruhige Schritte gerundet, damit die Anzeige nicht flackert.
String inMetern(double m) => m < 1000
    ? 'In ${(m / 50).round() * 50} m'
    : 'In ${(m / 1000).toStringAsFixed(1).replaceAll('.', ',')} km';
