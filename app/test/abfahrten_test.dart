import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/abfahrten.dart';
import 'package:schnellweg/api.dart';

Abfahrt _abfahrt(String linie, String ziel, DateTime dep) => Abfahrt.json({
      'line': linie,
      'headsign': ziel,
      'trip_id': '$linie-$ziel',
      'dep': dep.toUtc().toIso8601String(),
    });

Widget _bereich(List<Abfahrt>? l, {String? fehler}) => MaterialApp(
      home: Scaffold(body: HaltAbfahrten(abfahrten: l, fehler: fehler, onRefresh: () async {})),
    );

void main() {
  test('Bahnsteige: nur gleicher Name, angetippter Halt zuerst', () {
    final h = Halt('b', 'Aachen Elisenbrunnen H2', 50.77, 6.08);
    final l = bahnsteigeVon(h, [
      Halt('a', 'Aachen Elisenbrunnen H1', 50.77, 6.08),
      h,
      Halt('c', 'Aachen Kapuzinergraben H1', 50.77, 6.08),
    ]);
    expect(l.map((x) => x.id), ['b', 'a']);
    expect(halteOrt(h, l).name, 'Aachen Elisenbrunnen');
  });

  test('Bahnsteige: nicht mehr als der Server annimmt', () {
    final h = Halt('x', 'Aachen Bushof H1', 50.77, 6.08);
    expect(bahnsteigeVon(h, [for (var i = 0; i < 40; i++) Halt('$i', 'Aachen Bushof H1', 50.77, 6.08)]).length, 30);
  });

  testWidgets('Bereich zeigt Laden, Abfahrten, Leer und Fehler', (t) async {
    await t.pumpWidget(_bereich(null));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await t.pumpWidget(_bereich([_abfahrt('33', 'Vaals', DateTime.now().add(const Duration(minutes: 5)))]));
    expect(find.textContaining('Vaals'), findsOneWidget);

    await t.pumpWidget(_bereich([]));
    expect(find.textContaining('Keine Abfahrten'), findsOneWidget);

    await t.pumpWidget(_bereich(null, fehler: 'Abfahrten nicht geladen (netz)'));
    expect(find.textContaining('nicht geladen'), findsOneWidget);
  });
}
