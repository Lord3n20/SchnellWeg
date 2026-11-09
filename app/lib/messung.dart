// Aufzeichnung des Messgangs (D-45): Der Android-Dienst schreibt jede Position in eine Datei, die Seite liest sie mit.
// So läuft die Messung bei ausgeschaltetem Bildschirm weiter und übersteht auch das Schließen der App.
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'hang_messung.dart';
import 'standort.dart';

const _genauMax = 20.0; // m; ungenauere Positionen zählen nicht
const _spurMax = 15000;
const _lueckeMax = 5000.0; // ms; größere Lücken im GPS werden nicht überbrückt
const messZeitMin = Duration(minutes: 2), messStreckeMin = 150.0;

/// Mittelt das Tempo in Bewegung aus der Aufzeichnung; [grenze]: (min, max) m/s, was als Bewegung zählt.
class MessStand {
  final (double, double) grenze;
  MessStand(this.grenze);
  Fix? _letzter;
  bool genau = false, pausiert = false; // genau: schon eine brauchbare Position?
  double gehMs = 0, strecke = 0, jetzt = 0; // ms in Bewegung, m, m/s
  final spur = <SpurPunkt>[];

  double get kmh => gehMs == 0 ? 0 : strecke / gehMs * 3600;
  bool get reicht => gehMs >= messZeitMin.inMilliseconds && strecke >= messStreckeMin;

  void aufnehmen(Object x) {
    if (x is MessPause) {
      pausiert = x.an;
      _letzter = null; // keine Strecke über die Pause hinweg
    } else if (x is Fix && !pausiert) {
      _fix(x);
    }
  }

  /// Unterbricht die Strecke, zum Beispiel um eine Busfahrt herum.
  void luecke() => _letzter = null;

  void _fix(Fix f) {
    if (f.genau > _genauMax) {
      _letzter = null; // keine Strecke über ungenaue Stellen hinweg
      return;
    }
    final a = _letzter;
    _letzter = f;
    genau = true;
    if (a == null) return;
    final dt = f.zeit - a.zeit;
    if (dt <= 0 || dt > _lueckeMax) return;
    final v = f.tempo >= 0 ? f.tempo : _abstand(a, f) / dt * 1000;
    jetzt = v;
    if (v < grenze.$1 || v > grenze.$2) return;
    gehMs += dt;
    strecke += v * dt / 1000;
    if (spur.length < _spurMax) spur.add(SpurPunkt(f.lat, f.lon, v * dt / 1000, dt / 1000));
  }
}

double _abstand(Position a, Position b) => messAbstand(a.lat, a.lon, b.lat, b.lon);

/// Abstand in Metern (Haversine).
double messAbstand(double aLat, double aLon, double bLat, double bLon) {
  const r = 6371000.0, rad = pi / 180;
  final dLat = (bLat - aLat) * rad, dLon = (bLon - aLon) * rad;
  final h = pow(sin(dLat / 2), 2) + cos(aLat * rad) * cos(bLat * rad) * pow(sin(dLon / 2), 2);
  return 2 * r * asin(sqrt(h));
}

class MessPause {
  final bool an;
  const MessPause(this.an);
}

/// Kopfzeile: art k = Kalibrieren, l = Route lernen (beide mit Profil), r = Route als JSON.
class MessKopf {
  final String art, wert;
  const MessKopf(this.art, this.wert);
}

/// Uhrzeit (ms) zu einem Stand von Fix.zeit.
class MessUhr {
  final double uhr, zeit;
  const MessUhr(this.uhr, this.zeit);
}

/// Eine Zeile der Datei: [Fix], [MessPause], [MessKopf], [MessUhr] oder null (Unlesbares).
Object? messZeile(String z) {
  if (z == 'p') return const MessPause(true);
  if (z == 'w') return const MessPause(false);
  if (z.length > 2 && z[1] == ',' && 'klr'.contains(z[0])) return MessKopf(z[0], z.substring(2));
  if (z.startsWith('t,')) {
    final t = z.split(',').skip(1).map(double.tryParse).toList();
    return t.length == 2 && !t.contains(null) ? MessUhr(t[0]!, t[1]!) : null;
  }
  if (!z.startsWith('f,')) return null;
  final t = z.split(',').skip(1).map(double.tryParse).toList();
  if (t.length != 5 || t.contains(null)) return null;
  return Fix(t[0]!, t[1]!, t[2]!, t[3]!, t[4]!);
}

/// Liest, was seit dem letzten Aufruf dazugekommen ist; nur vollständige Zeilen.
class MessLeser {
  final File datei;
  int _pos = 0;
  MessLeser(this.datei);

  Future<List<Object>> neue() async {
    if (!await datei.exists()) return const [];
    final f = await datei.open();
    try {
      final rest = await f.length() - _pos;
      if (rest <= 0) return const [];
      await f.setPosition(_pos);
      final daten = await f.read(rest.clamp(1, 1 << 24)); // die Messseite liest jede Sekunde: nicht jedes Mal 16 MB anfordern
      final ende = daten.lastIndexOf(10);
      if (ende < 0) return const [];
      _pos += ende + 1;
      return [
        for (final z in const LineSplitter().convert(utf8.decode(daten.sublist(0, ende), allowMalformed: true)))
          if (messZeile(z) case final Object x) x
      ];
    } finally {
      await f.close();
    }
  }
}

class Messung {
  static const _kanal = MethodChannel('schnellweg/messung');

  static Future<File> datei() async => File('${(await getApplicationSupportDirectory()).path}/messung.txt');

  /// Schlüssel des Profils einer angefangenen Messung, sonst null.
  static Future<String?> offen() async {
    try {
      final f = await datei();
      if (!await f.exists()) return null;
      final erste = await f.openRead(0, 200).transform(utf8.decoder).transform(const LineSplitter()).first;
      return erste.startsWith('k,') ? erste.substring(2) : null;
    } on Exception {
      return null;
    }
  }

  /// Läuft oder liegt eine Aufzeichnung einer Route?
  static Future<bool> routeOffen() async {
    try {
      final f = await datei();
      if (!await f.exists()) return false;
      return (await f.openRead(0, 2).transform(utf8.decoder).first).startsWith('l,');
    } on Exception {
      return false;
    }
  }

  /// Fehler als PlatformException: „erlaubnis“ oder „aus“. Mit [route] (JSON) wird aus der Fahrt gelernt, [ziel] beendet sie bei Ankunft.
  static Future<void> starten(String profil, String titel, {String? route, (double, double)? ziel}) => _kanal.invokeMethod('starten', {
        'profil': profil,
        'titel': titel,
        'route': ?route,
        if (ziel != null) 'lat': ziel.$1,
        if (ziel != null) 'lon': ziel.$2,
      });

  /// Beenden und im Hintergrund auswerten, wie über die Benachrichtigung.
  static Future<void> abschliessen() => _kanal.invokeMethod('abschliessen');
  static Future<void> anhalten() => _kanal.invokeMethod('beenden');
  static Future<void> pausieren(bool an) => _kanal.invokeMethod('pause', an);
  static Future<bool> laeuft() async => await _kanal.invokeMethod<bool>('laeuft') ?? false;

  /// Hält an und löscht die Aufzeichnung.
  static Future<void> verwerfen() async {
    await anhalten();
    final f = await datei();
    if (await f.exists()) await f.delete();
  }
}
