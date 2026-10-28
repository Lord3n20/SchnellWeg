import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/karte.dart';

void main() {
  test('Kartenstil behält die Beschriftungen', () {
    final namen = hellerStil.layers.map((l) => l.id).toList();
    expect(namen, contains('places_locality'));
    expect(namen, contains('roads_labels_minor'));
  });
}
