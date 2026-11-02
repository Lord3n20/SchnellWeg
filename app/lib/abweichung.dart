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
  // Abschnitt statt nächster Stützpunkt: Auf einem langen geraden Stück liegt der Abzweig sonst schon lange vorher am nächsten und sein Hinweis fiele weg
  final hier = aufWeg(form, lat, lon);
  for (final s in schritte) {
    if (s.i <= hier.i || s.i >= form.length) continue;
    var meter = entfernung(hier.punkt.latitude, hier.punkt.longitude, form[hier.i + 1].latitude, form[hier.i + 1].longitude);
    for (var i = hier.i + 1; i < s.i; i++) {
      meter += entfernung(form[i].latitude, form[i].longitude, form[i + 1].latitude, form[i + 1].longitude);
    }
    if (meter < 1 && s.i == form.length - 1) return null; // am Ende angekommen
    return (s, meter);
  }
  return null;
}

/// „In 350 m“ / „In 2,4 km“, auf ruhige Schritte gerundet, damit die Anzeige nicht flackert.
String inMetern(double m) => m < 1000
    ? 'In ${(m / 50).round() * 50} m'
    : 'In ${(m / 1000).toStringAsFixed(1).replaceAll('.', ',')} km';

/// Nächste Stelle auf dem Linienzug: Abschnitt [i] (von Punkt i nach i + 1), Lotpunkt und Abstand in Metern.
({int i, LatLng punkt, double m}) aufWeg(List<LatLng> weg, double lat, double lon) {
  final mLat = 111320.0, mLon = 111320.0 * cos(lat * pi / 180);
  var best = (i: 0, punkt: weg.first, m: double.infinity);
  for (var i = 0; i < weg.length; i++) {
    final ax = (weg[i].longitude - lon) * mLon, ay = (weg[i].latitude - lat) * mLat;
    var t = 0.0, dx = 0.0, dy = 0.0;
    if (i + 1 < weg.length) {
      dx = (weg[i + 1].longitude - lon) * mLon - ax;
      dy = (weg[i + 1].latitude - lat) * mLat - ay;
      final l = dx * dx + dy * dy;
      if (l > 0) t = (-(ax * dx + ay * dy) / l).clamp(0.0, 1.0);
    }
    final x = ax + t * dx, y = ay + t * dy, m = sqrt(x * x + y * y);
    if (m < best.m) best = (i: i, punkt: LatLng(lat + y / mLat, lon + x / mLon), m: m);
  }
  return best;
}

/// Meter vom Lotpunkt bis zum Ende des Linienzugs.
double restMeter(List<LatLng> weg, ({int i, LatLng punkt, double m}) hier) {
  if (hier.i + 1 >= weg.length) return 0;
  var m = entfernung(hier.punkt.latitude, hier.punkt.longitude, weg[hier.i + 1].latitude, weg[hier.i + 1].longitude);
  for (var i = hier.i + 1; i + 1 < weg.length; i++) {
    m += entfernung(weg[i].latitude, weg[i].longitude, weg[i + 1].latitude, weg[i + 1].longitude);
  }
  return m;
}

/// Kurs von a nach b in Grad, 0 = Norden, im Uhrzeigersinn.
double kurs(LatLng a, LatLng b) {
  final x = (b.longitude - a.longitude) * cos(a.latitude * pi / 180), y = b.latitude - a.latitude;
  return (atan2(x, y) * 180 / pi + 360) % 360;
}

/// Zwischen zwei Kursen über den kürzeren Bogen (350° → 10° läuft über 0°).
double kursMisch(double von, double nach, double t) {
  final d = ((nach - von + 540) % 360) - 180;
  return (von + d * t + 360) % 360;
}

/// Zoomstufe nach Tempo: je schneller, desto weiter voraus sieht man.
double fahrZoom(double kmh) => kmh < 40 ? 17 : (kmh < 90 ? 16 : 15);
