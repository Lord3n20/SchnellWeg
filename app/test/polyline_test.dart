import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';

void main() {
  test('polyline: Beispiel aus der Formatbeschreibung (Genauigkeit 5)', () {
    final p = polyline(r'_p~iF~ps|U_ulLnnqC_mqNvxq`@', stellen: 5);
    expect(p, [(38.5, -120.2), (40.7, -120.95), (43.252, -126.453)]);
  });
  test('polyline: kaputte Eingabe ergibt leer', () {
    expect(polyline('_p~iF~'), isEmpty);
    expect(polyline(''), isEmpty);
  });
}
