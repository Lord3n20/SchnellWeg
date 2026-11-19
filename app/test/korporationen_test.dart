import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/korporationen.dart';

void main() {
  final liste = Korporationen.lesen(File('assets/korporationen.json').readAsStringSync());

  test('Liste aus dem App-Paket liegt in der Region', () {
    expect(liste.length, greaterThan(20));
    for (final k in liste) {
      expect(k.name, isNotEmpty);
      expect(k.lat, inInclusiveRange(50.38, 51.12));
      expect(k.lon, inInclusiveRange(5.45, 7.25));
    }
  });

  test('nächste vier nach Abstand, Grenze greift', () {
    Korporationen.instanz.liste = liste;
    final nah = Korporationen.instanz.naechste(50.7753, 6.0839); // Aachen Hbf
    expect(nah.length, 4);
    for (var i = 1; i < nah.length; i++) {
      expect(nah[i].$2, greaterThanOrEqualTo(nah[i - 1].$2));
    }
    expect(Korporationen.instanz.naechste(52.52, 13.40), isEmpty); // Berlin: nichts in 25 km
  });

  test('Suche über Name und Anschrift', () {
    Korporationen.instanz.liste = liste;
    expect(Korporationen.instanz.suchen('nizzaallee').length, greaterThanOrEqualTo(2));
    expect(Korporationen.instanz.suchen(' '), isEmpty);
  });

  test('Farben mit Perkussion und unbekannten Namen', () {
    final (s, p) = farbenLesen('blue-white-orange@white');
    expect(s.length, 3);
    expect(p, isNotNull);
    expect(farbenLesen('black|white-quatsch').$1.length, 1);
    expect(farbenLesen('').$1, isEmpty);
  });

  test('Abstand ungefähr richtig', () {
    // Hbf → Elisenbrunnen ~ 650 m
    expect(meterZwischen(50.7677, 6.0912, 50.7741, 6.0859), inInclusiveRange(700, 900));
  });
}
