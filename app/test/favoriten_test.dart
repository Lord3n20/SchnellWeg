import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/favoriten.dart';

Ort ort(String n, double lat) => Ort('ort', n, '', lat, 6);

String folge() => [
      for (final e in Favoriten.instanz.eintraege) e is FavOrdner ? '[${e.name}]' : (e as Ort).name,
    ].join(' ');

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final dir = Directory.systemTemp.createTempSync('fav');
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
      const MethodChannel('plugins.flutter.io/path_provider'), (_) async => dir.path);
  final fav = Favoriten.instanz;

  setUp(() => fav.eintraege = [
        ort('a', 1), ort('b', 2), const FavOrdner('Uni'), ort('c', 3), ort('d', 4), const FavOrdner('Sport'), ort('e', 5),
      ]);

  test('Gruppen', () {
    expect(fav.oben.map((o) => o.name), ['a', 'b']);
    expect([for (final (n, l) in fav.ordner) '$n:${l.length}'], ['Uni:2', 'Sport:1']);
  });

  test('Favorit in Ordner ziehen und neu anlegen', () async {
    await fav.ziehen(0, 3); // a hinter c
    expect(folge(), 'b [Uni] c a d [Sport] e');
    await fav.umschalten(ort('f', 6));
    expect(folge(), 'b f [Uni] c a d [Sport] e');
  });

  test('Ordner zieht seinen Inhalt mit, nie in einen anderen Ordner', () async {
    await fav.ziehen(5, 2); // Sport vor Uni
    expect(folge(), 'a b [Sport] e [Uni] c d');
    await fav.ziehen(2, 5); // Sport zurück mitten in Uni → landet dahinter
    expect(folge(), 'a b [Uni] c d [Sport] e');
    await fav.ziehen(5, 0); // ganz nach oben → hinter die Favoriten ohne Ordner
    expect(folge(), 'a b [Sport] e [Uni] c d');
  });

  test('verschieben, auflösen, speichern', () async {
    await fav.inOrdner(fav.eintraege[0] as Ort, 'Sport');
    expect(folge(), 'b [Uni] c d [Sport] e a');
    await fav.ordnerAufloesen('Uni');
    expect(folge(), 'b c d [Sport] e a');
    final gespeichert = folge();
    fav.eintraege = [];
    await (fav..resetFuerTest()).laden();
    expect(folge(), gespeichert);
  });
}
