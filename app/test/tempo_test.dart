import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/tempo.dart';

/// Wie aus der Datei: über echtes JSON, damit die Map-Typen stimmen.
Tempo _ausJson(Map<String, dynamic> j) => Tempo.ausJson(jsonDecode(jsonEncode(j)) as Map<String, dynamic>);

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

  group('Eigene Profile (D-34)', () {
    test('alter Stand wird zu eigenen Profilen mit denselben Regeln', () {
      final t = _ausJson({
        'aktiv': 'Rennrad',
        'profile': {
          'Rennrad': {'kmh': 28.0, 'gemessen': '2026-10-28T10:00:00.000'},
          'Einrad': {'kmh': 13.0, 'titel': 'Inliner'},
        },
      });
      expect(t.aktiv, 'Rennrad');
      final r = t.aktuell;
      expect((r.art, r.kmh, r.ticket, r.gemessen != null), ('eigen', 28.0, true, true));
      expect(r.serverArt, isNull);
      expect(r.serverMeiden, {'fusswege': true, 'treppen': 1.0, 'belag': 0.8, 'bus': true});
      expect(t.profile.firstWhere((p) => p.name == 'Klapprad').meiden['bus_min'], 5);
      final e = t.profile.firstWhere((p) => p.name == 'Einrad');
      expect(e.anzeige, 'Inliner');
      expect(e.serverMeiden, isEmpty);
      expect(t.profile.firstWhere((p) => p.name == 'Einrad schnell').anzeige, 'Custom 2');
    });

    test('neuer Stand: gelöschte bleiben weg, Reihenfolge und Werte bleiben', () {
      final j = {
        'v': 2,
        'aktiv': 'p1',
        'profile': {
          'entspannt': {'art': 'fuss', 'kmh': 5.0, 'meiden': {'treppen': 0.5}},
          'Auto': {'art': 'auto', 'kmh': 100.0},
          'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa', 'meiden': {'oev': true}, 'fern': true},
        },
      };
      final t = _ausJson(j);
      expect(t.profile.map((p) => p.name), ['entspannt', 'Auto', 'p1']);
      expect((t.aktuell.anzeige, t.aktuell.kmh, t.aktuell.fern), ('Mofa', 40.0, true));
      final f = t.profile.first;
      expect(f.serverArt, isNull); // zu Fuß mit Meiden: Regeln aus meiden
      expect(f.serverMeiden, {'treppen': 0.5});
      expect(t.profile[1].serverArt, 'auto');
      final k = t.aktuell.kopie()..setzen('oev', false);
      expect(k.meiden, isEmpty);
      expect(t.aktuell.meiden, {'oev': true});
      expect(k.gleich(t.aktuell), isFalse);
    });

    test('Neuinstallation: nur Zu Fuß und Auto', () {
      final t = _ausJson({'v': 2, 'aktiv': 'entspannt', 'profile': {}});
      expect(t.profile.map((p) => p.anzeige), ['Zu Fuß', 'Auto']);
      expect((t.profile.first.serverArt, t.profile.first.serverMeiden), ('fuss', null));
    });
  });
}
