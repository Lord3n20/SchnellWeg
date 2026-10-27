// Standort über Androids eigenen LocationManager (MainActivity.kt) statt geolocator & Co.,
// die die Play-Dienste einbinden. Kein Dauer-Tracking: nur eine Position, wenn die App danach fragt.
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
}
