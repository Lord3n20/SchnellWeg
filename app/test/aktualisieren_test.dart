import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/aktualisieren.dart';
import 'package:schnellweg/api.dart';

void main() {
  test('nur ein höherer Code ist ein Update', () {
    final s = AppStand('0.2.0', 2, 'a' * 64, 10);
    expect(istNeuer(s, 1), isTrue);
    expect(istNeuer(s, 2), isFalse);
    expect(istNeuer(s, 3), isFalse);
  });

  test('Serverantwort lesen', () {
    final s = AppStand.json({'version': '0.2.0', 'code': 2, 'sha256': 'abc', 'bytes': 59177813});
    expect((s.version, s.code, s.sha256, s.bytes), ('0.2.0', 2, 'abc', 59177813));
  });
}
