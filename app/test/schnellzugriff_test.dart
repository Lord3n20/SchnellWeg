import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/schnellzugriff.dart';
import 'package:schnellweg/standort.dart';

void main() {
  final kino = Ort('poi', 'Kino', '', 50.77, 6.08);
  final uni = Ort('gebaeude', 'Audimax', '', 50.78, 6.07);
  final hier = Position(50.75, 6.1, 5);

  group('tauschen', () {
    test('vom eigenen Standort: Ziel wird Start, Standort wird Ziel', () {
      final (start, ziel) = tauschen(null, kino, hier)!;
      expect(start, kino);
      expect(ziel.art, artHier);
      expect((ziel.lat, ziel.lon), (50.75, 6.1));
    });

    test('zweimal tauschen führt zurück zum eigenen Standort', () {
      final (s1, z1) = tauschen(null, kino, hier)!;
      final (s2, z2) = tauschen(s1, z1, hier)!;
      expect(s2, isNull);
      expect(z2, kino);
    });

    test('mit gewähltem Start tauschen beide die Plätze', () {
      final (start, ziel) = tauschen(uni, kino, null)!;
      expect(start, kino);
      expect(ziel, uni);
    });

    test('ohne bekannten Standort kein Rückweg dorthin', () {
      expect(tauschen(null, kino, null), isNull);
    });
  });

  group('verbindungText', () {
    String zeit(int h, int m) => DateTime(2026, 11, 9, h, m).toUtc().toIso8601String();

    Map<String, dynamic> fahrt(Map<String, dynamic> mehr) => {
          'mode': 'ride',
          'from': {'id': 'a', 'name': 'Ponttor'},
          'to': {'id': 'b', 'name': 'Elisenbrunnen'},
          'dep': zeit(17, 12),
          'arr': zeit(17, 25),
          'line': '33',
          ...mehr,
        };

    Verbindung v(List<Map<String, dynamic>> legs, {String? anRt}) => Verbindung.json({
          'trips': legs.length,
          'dep': zeit(17, 5),
          'arr': zeit(17, 30),
          'arr_rt': anRt,
          'legs': legs,
        });

    test('zeigt Ankunft, Dauer und die Fahrt', () {
      expect(verbindungText(v([fahrt({})]), 'Kino'),
          'Ankunft 17:30 · Kino\nLos 17:05 · 25 min · direkt\n17:12 33: Ponttor → Elisenbrunnen, an 17:25');
    });

    test('nimmt die Echtzeit und nennt Ausfälle', () {
      final t = verbindungText(v([fahrt({'dep_rt': zeit(17, 15), 'arr_rt': zeit(17, 28), 'canceled': true})], anRt: zeit(17, 33)), 'Kino');
      expect(t, startsWith('Ankunft 17:33 · Kino'));
      expect(t, endsWith('17:15 33: Ponttor → Elisenbrunnen, an 17:28 (fällt aus)'));
    });

    test('reiner Fußweg hat keine Fahrtzeile', () {
      final t = verbindungText(v([{'mode': 'walk', 'dep': zeit(17, 5), 'arr': zeit(17, 30)}]), 'Kino');
      expect(t.split('\n'), hasLength(2));
    });

    test('spätere Verbindungen suchen nach der letzten Abfahrt mit Fahrt', () {
      Verbindung ab(int m, int fahrten) =>
          Verbindung.json({'trips': fahrten, 'dep': zeit(17, m), 'arr': zeit(18, 0), 'legs': <Map<String, dynamic>>[]});
      final jetzt = DateTime(2026, 11, 9, 17, 0);
      expect(spaeterAb([ab(5, 1), ab(20, 2), ab(40, 0)], jetzt), DateTime(2026, 11, 9, 17, 21));
      expect(spaeterAb([ab(40, 0)], jetzt), isNull);
      expect(spaeterAb([ab(5, 1)], DateTime(2026, 11, 9, 17, 30)), DateTime(2026, 11, 9, 17, 31));
    });
  });
}
