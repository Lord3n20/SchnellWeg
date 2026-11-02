import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/tempo.dart';

void main() {
  const min10 = Duration(minutes: 10);
  test('Gehhinweis: Puffer in beide Richtungen', () {
    expect(gehHinweis(min10, const Duration(minutes: 20), 5)?.$1, GehStufe.entspannter);
    expect(gehHinweis(min10, const Duration(minutes: 20), 5)?.$2, 2.5);
    expect(gehHinweis(min10, min10, 5)?.$1, GehStufe.normal);
    expect(gehHinweis(min10, const Duration(minutes: 12), 5)?.$1, GehStufe.normal); // 4,17 km/h: im Puffer
    expect(gehHinweis(min10, const Duration(minutes: 9), 5)?.$1, GehStufe.normal); // 5,56 km/h: im Puffer
    expect(gehHinweis(min10, const Duration(minutes: 5), 5)?.$1, GehStufe.schneller);
    expect(gehHinweis(min10, Duration.zero, 5), isNull);
  });

  group('Umstellung auf ein Gehtempo', () {
    Map<String, dynamic> stand(String aktiv) => {
          'aktiv': aktiv,
          'profile': {
            'entspannt': {'kmh': 4.5, 'gemessen': '2026-10-28T10:00:00.000'},
            'normal': {'kmh': 5.2},
            'schnell': {'kmh': 6.1},
            'Einrad': {'kmh': 13.0, 'titel': 'Inliner'},
          },
        };

    test('aktives altes Fuß-Profil gibt das Tempo vor', () {
      final t = Tempo.ausJson(stand('schnell'));
      expect(t.aktiv, 'entspannt');
      expect(t.aktuell.kmh, 6.1);
      expect(t.aktuell.gemessen, isNull);
      expect(t.profile.where((p) => p.art == 'fuss').length, 1);
    });

    test('sonst bleibt das bisherige Gehtempo samt Messung; Custom bleibt', () {
      final t = Tempo.ausJson(stand('Einrad'));
      expect(t.aktiv, 'Einrad');
      expect(t.aktuell.titel, 'Inliner');
      final f = t.profile.firstWhere((p) => p.art == 'fuss');
      expect(f.kmh, 4.5);
      expect(f.gemessen, isNotNull);
      expect(f.anzeige, 'Zu Fuß');
    });
  });
}
