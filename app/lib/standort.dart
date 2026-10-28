// Standort über Androids eigenen LocationManager (MainActivity.kt) statt geolocator & Co.,
// die die Play-Dienste einbinden. Kein Dauer-Tracking: eine Position, wenn die App danach fragt;
// laufend nur während des Tempo-Messgangs (Seite offen, Bildschirm an), nichts wird gespeichert.
import 'package:flutter/services.dart';

class Position {
  final double lat, lon, genau; // genau = Radius in Metern
  Position(this.lat, this.lon, this.genau);
}

class Standort {
  static const _kanal = MethodChannel('schnellweg/standort');

  /// Aktuelle Position oder null (keine Berechtigung, Standort aus, Zeitüberschreitung).
  static Future<Position?> holen() async {
    try {
      final m = await _kanal.invokeMapMethod<String, double>('holen');
      if (m == null) return null;
      return Position(m['lat']!, m['lon']!, m['genau']!);
    } on PlatformException {
      return null;
    }
  }

  static const _laufend = EventChannel('schnellweg/standort/laufend');

  /// GPS etwa jede Sekunde, solange abonniert (nur für den Messgang). Fehler: „erlaubnis“ oder „aus“.
  static Stream<Fix> laufend() => _laufend.receiveBroadcastStream().map((e) {
        final m = (e as Map).cast<String, double>();
        return Fix(m['lat']!, m['lon']!, m['genau']!, m['zeit']!, m['tempo']!);
      });
}

/// Eine laufende GPS-Position: zeit in ms (Systemuhr ab Start), tempo in m/s aus dem Doppler (< 0 = keins).
class Fix extends Position {
  final double zeit, tempo;
  Fix(super.lat, super.lon, super.genau, this.zeit, this.tempo);
}
