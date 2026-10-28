import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/losgehen.dart';

// Zeiten in Ortszeit erzeugen, damit der Test in jeder Zeitzone gilt (CI läuft in UTC).
String _z(int h, int m) => DateTime(2026, 10, 28, h, m).toUtc().toIso8601String();

void main() {
  final t = Termin('Seminar', 'Templergraben 55', DateTime(2026, 10, 28, 14));
  test('Ort nicht gefunden', () {
    expect(losText(Losgehplan(t, null, null)), 'Ort „Templergraben 55“ nicht gefunden');
  });
  test('keine Verbindung', () {
    expect(losText(Losgehplan(t, Ort('adresse', 'Templergraben 55', '', 50.77, 6.08), null)), 'Keine Verbindung bis 14:00');
  });
  test('mit Fahrt', () {
    final v = Verbindung.json({
      'trips': 1,
      'dep': _z(13, 32),
      'arr': _z(13, 52),
      'legs': [
        {'mode': 'walk', 'dep': _z(13, 32), 'arr': _z(13, 38)},
        {
          'mode': 'ride', 'line': '33', 'headsign': 'Vaals',
          'from': {'id': 'a', 'name': 'Aachen, Ponttor'}, 'to': {'id': 'b', 'name': 'Aachen, Elisenbrunnen'},
          'dep': _z(13, 38), 'arr': _z(13, 48),
        },
      ],
    });
    expect(losText(Losgehplan(t, Ort('adresse', 'x', '', 0, 0), v)), 'Los 13:32 · Linie 33 ab Aachen, Ponttor 13:38 · an 13:52');
  });
}
