import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/hang_messung.dart';

Stueck st(double g, double m, double kmh) => Stueck(g, m, m / kmh * 3.6);

void main() {
  test('Kachelname', () {
    expect(kachelName(50.77, 6.08), 'N50E006');
    expect(kachelName(-0.5, -1.5), 'S01W002');
  });

  test('Höhe wird zwischen den Punkten gemittelt, Lücken bleiben Lücken', () {
    final b = ByteData(8)
      ..setInt16(0, 100)
      ..setInt16(2, 200)
      ..setInt16(4, 300)
      ..setInt16(6, 400);
    final k = HoehenKachel(b.buffer.asUint8List(), 50, 6, seite: 2);
    expect(k.hoehe(51, 6), 100); // Nordwesten
    expect(k.hoehe(50.5, 6.5), 250);
    b.setInt16(0, -32768);
    expect(k.hoehe(50.5, 6.5), isNull);
  });

  test('Spur wird zu Abschnitten mit Steigung', () {
    var h = 200.0;
    final spur = [for (var i = 0; i < 11; i++) SpurPunkt(50.0 + i, 6, 10, 2)];
    final s = stuecke(spur, (la, _) {
      h = 200 + (la - 50) * 0.5; // 5 % auf 10 m je Punkt
      return h;
    });
    expect(s.length, 2);
    expect(s.first.g, closeTo(5, 1e-9));
    expect(s.first.kmh, closeTo(18, 1e-9));
  });

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

  test('Zu Fuß setzt nie „zu Fuß ab“', () {
    expect(hangAuswerten([st(0, 300, 5), st(5, 200, 3)], 4.5, eigen: false).zuFussAb, isNull);
  });
}
