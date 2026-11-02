import 'package:flutter_test/flutter_test.dart';
import 'package:latlong2/latlong.dart';
import 'package:schnellweg/abweichung.dart';

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
}
