import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/darstellung.dart';

void main() {
  test('Custom-Thema nimmt die Grundfarben', () {
    final t = customThema(customVorgabe);
    expect(t.brightness, Brightness.dark);
    expect(t.colorScheme.primary, const Color(0xFF00FFFF));
    expect(t.colorScheme.onPrimary, Colors.black);
    expect(t.colorScheme.error, const Color(0xFFFF3B30));
    expect(t.scaffoldBackgroundColor, const Color(0xFF05090D));
  });
}
