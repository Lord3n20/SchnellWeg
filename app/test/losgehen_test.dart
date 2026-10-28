import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/losgehen.dart';

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
      'dep': '2026-10-28T13:32:00+01:00',
      'arr': '2026-10-28T13:52:00+01:00',
      'legs': [
        {'mode': 'walk', 'dep': '2026-10-28T13:32:00+01:00', 'arr': '2026-10-28T13:38:00+01:00'},
        {
          'mode': 'ride', 'line': '33', 'headsign': 'Vaals',
          'from': {'id': 'a', 'name': 'Aachen, Ponttor'}, 'to': {'id': 'b', 'name': 'Aachen, Elisenbrunnen'},
          'dep': '2026-10-28T13:38:00+01:00', 'arr': '2026-10-28T13:48:00+01:00',
        },
      ],
    });
    expect(losText(Losgehplan(t, Ort('adresse', 'x', '', 0, 0), v)), 'Los 13:32 · Linie 33 ab Aachen, Ponttor 13:38 · an 13:52');
  });
}
