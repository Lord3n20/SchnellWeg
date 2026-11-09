import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/hang_messung.dart';

Stueck st(double g, double m, double kmh, [int c = 0]) => Stueck(g, m, m / kmh * 3.6, c);

void main() {
  test('Longboard: bergauf wird gelaufen, bergab schneller', () {
    final e = hangAuswerten([st(0, 300, 12), st(1, 100, 12), st(3, 100, 10), st(5, 100, 4.5), st(7, 100, 4), st(-6, 150, 24)], 11, eigen: true);
    expect(e.kmhEben, 12);
    expect(e.zuFussAb, 5);
    expect(e.auf, 0); // ersetzt durch „zu Fuß ab“
    expect(e.ab, 100);
  });

  test('Kein Laufen bergauf: Prozent auf volle Steigung hochgerechnet', () {
    final e = hangAuswerten([st(0, 300, 20), st(3, 200, 15)], 18, eigen: true);
    expect(e.zuFussAb, 0);
    expect(e.auf, -50); // 3 %: −25 %, bei 6 % doppelt
  });

  test('Zu wenig Hang: Regler bleiben', () {
    final e = hangAuswerten([st(0, 300, 12), st(4, 40, 8), st(-4, 40, 16)], 12, eigen: true);
    expect((e.auf, e.ab, e.zuFussAb), (null, null, null));
  });

  test('Zu wenig Ebene: Schnitt der Messung gilt', () {
    expect(hangAuswerten([st(4, 200, 8)], 9.3, eigen: false).kmhEben, 9.3);
  });

  test('Belag: gemessenes Tempo wird Prozent oder Fuß-Profil', () {
    final e = hangAuswerten([st(0, 300, 12), st(0, 150, 4, 2), st(1, 150, 6, 4), st(0, 150, 12, 5), st(0, 40, 2, 1)], 11, eigen: true, fuss: 'entspannt');
    expect(e.belag, {2: 'entspannt', 4: -50, 5: null});
    expect(e.kmhEben, 12); // schlechter Belag zählt nicht zur Ebene
  });

  test('Ohne rollendes Profil kein Fuß-Profil, nur Prozent', () {
    final e = hangAuswerten([st(0, 300, 5), st(0, 150, 3, 2)], 5, eigen: false, fuss: 'entspannt');
    expect(e.belag, {2: -40});
  });

  test('Zu Fuß setzt nie „zu Fuß ab“', () {
    expect(hangAuswerten([st(0, 300, 5), st(5, 200, 3)], 4.5, eigen: false).zuFussAb, isNull);
  });
}
