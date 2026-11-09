// Aufzeichnung des Messgangs (D-45): Der Android-Dienst schreibt jede Position in eine Datei, die Seite liest sie mit.
// So läuft die Messung bei ausgeschaltetem Bildschirm weiter und übersteht auch das Schließen der App.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'standort.dart';

class MessPause {
  final bool an;
  const MessPause(this.an);
}

/// Eine Zeile der Datei: [Fix], [MessPause] oder null (Kopfzeile, Unlesbares).
Object? messZeile(String z) {
  if (z == 'p') return const MessPause(true);
  if (z == 'w') return const MessPause(false);
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
      await f.setPosition(_pos);
      final daten = await f.read(1 << 22);
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

  /// Fehler als PlatformException: „erlaubnis“ oder „aus“.
  static Future<void> starten(String profil, String titel) => _kanal.invokeMethod('starten', {'profil': profil, 'titel': titel});
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
