import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/hang_messung.dart';
import 'package:schnellweg/lernen.dart';
import 'package:schnellweg/tempo.dart';

void main() {
  test('Schwelle trennt genutzte und ausgelassene Busse', () {
    expect(busSchwelle([(12, true), (9, true), (3, false), (2, false)]), 4);
    expect(busSchwelle([(5, true), (1, true)]), 0);
    expect(busSchwelle([(2, false), (20, false)]), 21);
  });

  test('Mischen addiert und kürzt nur das Alte', () {
    final m = lernMischen([[1, 0, 29000, 7000]], const [Stueck(1, 2000, 400)]);
    expect(m.length, 1);
    expect(m[0][2], closeTo(30000, 1));
    expect(m[0][3], closeTo(7000 * 28000 / 29000 + 400, 1));
  });

  test('Eintragen setzt die Bus-Minuten erst ab drei Beobachtungen und nie bei gemiedenem Bus', () {
    final p = TempoProfil('x', 'eigen', 15);
    busEintragen(p, spart: 10, genutzt: true, bus: true, strecke: 'a|b', warteS: 60);
    busEintragen(p, spart: 1, genutzt: false, bus: true, strecke: 'a|c');
    expect(p.meiden.containsKey('bus_min'), isFalse);
    busEintragen(p, spart: 8, genutzt: true, bus: true, strecke: 'a|b', warteS: 90);
    expect(p.meiden['bus_min'], 2);
    final q = TempoProfil('y', 'eigen', 15)..meiden['bus'] = true;
    for (var i = 0; i < 4; i++) {
      busEintragen(q, spart: 10, genutzt: true, bus: true, strecke: 'a|b');
    }
    expect(q.meiden.containsKey('bus_min'), isFalse);
  });

  test('Gelerntes übersteht Speichern und Laden', () {
    final p = TempoProfil('x', 'eigen', 15)..lern = [[1, 0, 100.0, 20.0]];
    busEintragen(p, spart: 5, genutzt: true, bus: true, strecke: 'a|b', warteS: 30);
    final k = p.kopie();
    expect(k.lern, p.lern);
    expect(k.busLern['strecken'], {'a|b': [1, 0]});
  });

  test('Wartegrenze erst ab fünf Beobachtungen', () {
    final p = TempoProfil('x', 'eigen', 15);
    for (var i = 0; i < 4; i++) {
      busEintragen(p, spart: 5, genutzt: true, bus: false, strecke: 'a|b', warteS: 240);
    }
    expect(warteGrenze(p), isNull);
    busEintragen(p, spart: 5, genutzt: true, bus: false, strecke: 'a|b', warteS: 300);
    expect(warteGrenze(p), 7);
  });
}
