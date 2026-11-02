import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';
import 'package:schnellweg/abweichung.dart';
import 'package:schnellweg/api.dart';

void main() {
  // Knapp 1,4 km nach Osten auf einer Breite; 0,001° Breite sind rund 111 m
  final weg = [const LatLng(50.77, 6.08), const LatLng(50.77, 6.10)];

  test('Abstand zum Linienzug', () {
    expect(abstandZumWeg(50.77, 6.09, weg), closeTo(0, 1));
    expect(abstandZumWeg(50.771, 6.09, weg), closeTo(111, 2));
    expect(abstandZumWeg(50.77, 6.11, weg), closeTo(705, 10)); // hinter dem Ende: Abstand zum Endpunkt
    expect(abstandZumWeg(50.771, 6.09, [const LatLng(50.77, 6.09)]), closeTo(111, 2)); // nur ein Punkt
    expect(abstandZumWeg(50.771, 6.09, [const LatLng(50.77, 6.09), const LatLng(50.77, 6.09)]), closeTo(111, 2));
  });

  test('erst der dritte Fix neben dem Weg zählt', () {
    final a = Abweichung(weg, schwelleSonst);
    expect(a.pruefen(50.771, 6.09, 5), isFalse);
    expect(a.pruefen(50.771, 6.09, 5), isFalse);
    expect(a.pruefen(50.771, 6.09, 5), isTrue);
  });

  test('zurück auf dem Weg setzt den Zähler zurück', () {
    final a = Abweichung(weg, schwelleSonst);
    a.pruefen(50.771, 6.09, 5);
    a.pruefen(50.771, 6.09, 5);
    expect(a.pruefen(50.77, 6.09, 5), isFalse);
    expect(a.pruefen(50.771, 6.09, 5), isFalse);
  });

  test('ungenaues GPS löst nichts aus, Genauigkeit erweitert die Schwelle', () {
    final a = Abweichung(weg, schwelleSonst);
    for (var i = 0; i < 5; i++) {
      expect(a.pruefen(50.78, 6.09, 80), isFalse);
    }
    final b = Abweichung(weg, schwelleAuto);
    for (var i = 0; i < 5; i++) {
      expect(b.pruefen(50.7709, 6.09, 45), isFalse); // 100 m daneben, aber 60 + 45 m erlaubt
    }
  });

  test('nächster Abbiegehinweis', () {
    final form = [for (var i = 0; i <= 10; i++) LatLng(50.77, 6.08 + i * 0.001)]; // je ~70 m
    Schritt s(String t, int i) => Schritt.json({'text': t, 'i': i});
    final l = [s('Los', 0), s('Rechts', 3), s('Ziel', 10)];
    final (a, m) = naechsterSchritt(form, l, 50.77, 6.081)!;
    expect(a.text, 'Rechts');
    expect(m, closeTo(141, 3));
    expect(naechsterSchritt(form, l, 50.77, 6.085)!.$1.text, 'Ziel');
    expect(naechsterSchritt(form, l, 50.77, 6.09), isNull);
    expect(naechsterSchritt(form, const [], 50.77, 6.081), isNull);
    expect(inMetern(340), 'In 350 m');
    expect(inMetern(2380), 'In 2,4 km');
  });
}
