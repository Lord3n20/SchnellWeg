import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/tempo.dart';

void main() {
  test('Autofahrt aus der Serverantwort', () {
    const zeit = {'dep': '2026-11-02T08:00:00+01:00', 'arr': '2026-11-02T08:50:00+01:00'};
    final v = Verbindung.json({
      'trips': 0,
      ...zeit,
      'legs': [
        {'mode': 'drive', 'from': null, 'to': null, ...zeit, 'shape': 'abc'},
      ],
    });
    expect(v.auto, isTrue);
    expect(v.abschnitte.single.fahrt, isFalse);
    expect(v.artText, 'mit dem Auto');
  });

  test('tempo.json ohne Auto-Profil behält Werte und Auswahl', () async {
    TestWidgetsFlutterBinding.ensureInitialized();
    final dir = Directory.systemTemp.createTempSync('tempo');
    addTearDown(() => dir.deleteSync(recursive: true));
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
        const MethodChannel('plugins.flutter.io/path_provider'), (_) async => dir.path);
    File('${dir.path}/tempo.json').writeAsStringSync(jsonEncode({
      'aktiv': 'Einrad',
      'profile': {
        'Einrad': {'kmh': 17.0, 'titel': 'Skates'},
      },
    }));
    final t = await Tempo.oeffnen();
    expect((t.aktiv, t.aktuell.kmh, t.aktuell.titel), ('Einrad', 17.0, 'Skates'));
    expect(t.profile.singleWhere((p) => p.art == 'auto').kmh, 130.0);
  });
}
