import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/api.dart';
import 'package:schnellweg/bummel.dart';
import 'package:schnellweg/korporationen.dart';

Korporation haus(String n, String v, double lat, double lon) => Korporation.json({'n': n, 'v': v, 'lat': lat, 'lon': lon});

void main() {
  final a = haus('Corps A', 'KSCV', 50.7760, 6.0840), b = haus('Corps B', 'KSCV', 50.7800, 6.0840);
  final c = haus('KDStV C', 'CV', 50.7770, 6.0840), d = haus('Weit weg', 'CV', 51.1, 6.9);
  final bm = Bummel.instanz;

  setUp(() {
    Korporationen.instanz.liste = [a, b, c, d];
    bm.werte = {};
    bm.vorbei = {};
    bm.besucht = [];
    bm.filter = BummelFilter();
    bm.zurueckPhase = false;
    bm.zuhause = null;
  });

  test('ohne Stimmen 50 %, Dachverband vererbt auf Häuser ohne Stimmen', () {
    expect(bm.rein(a, 'ADB'), closeTo(0.5, 1e-9));
    bm.werte = {'Corps A': HausWerte.json({'rein': {'ADB': [0, 4]}})};
    expect(bm.rein(a, 'ADB'), lessThan(0.2));
    expect(bm.rein(b, 'ADB'), lessThan(0.4)); // gleicher Dachverband, keine eigenen Stimmen
    expect(bm.rein(c, 'ADB'), closeTo(0.5, 1e-9)); // anderer Dachverband
    expect(bm.rein(a, 'CV'), greaterThan(bm.rein(a, 'ADB'))); // andere Gäste: nur das Grobe zählt
  });

  test('Skalen: Haus, sonst Dachverband, sonst unbekannt; relativ', () {
    bm.werte = {
      'Corps A': HausWerte.json({'presslust': [16, 2], 'lr': [8, 1]}),
      'KDStV C': HausWerte.json({'lr': [3, 1]}),
    };
    expect(bm.mittel(a, Skala.presslust), 8);
    expect(bm.mittel(b, Skala.presslust), 8);
    expect(bm.mittel(c, Skala.presslust), isNull);
    expect(bm.relativ(a, Skala.lr), 1.0);
    expect(bm.relativ(c, Skala.lr), 0.0);
  });

  test('Vorschläge: Filter, Umgangsverbot, besucht, Umkreis', () {
    bm.vorbei = {'Corps B': 0};
    var v = bm.vorschlaege(50.7753, 6.0839);
    expect(v.map((x) => x.$1.name), containsAll(['Corps A', 'KDStV C']));
    expect(v.map((x) => x.$1.name), isNot(contains('Corps B')));
    expect(v.map((x) => x.$1.name), isNot(contains('Weit weg')));
    bm.besucht = ['Corps A'];
    v = bm.vorschlaege(50.7753, 6.0839);
    expect(v.map((x) => x.$1.name), ['KDStV C']);
    // eingeschränkte Skala ohne Werte: nur mit „unbekannt zulassen“
    bm.besucht = [];
    bm.vorbei = {};
    bm.filter = BummelFilter(bereiche: {for (final s in Skala.values) s: const RangeValues(1, 10)}..[Skala.fechten] = const RangeValues(1, 4), unbekannt: false);
    expect(bm.vorschlaege(50.7753, 6.0839), isEmpty);
    bm.werte = {'Corps A': HausWerte.json({'fechten': [3, 1]})};
    expect(bm.vorschlaege(50.7753, 6.0839).map((x) => x.$1.name), ['Corps A', 'Corps B']); // B erbt vom KSCV
  });

  test('darf nicht fehlen zieht nach vorn', () {
    bm.vorbei = {'KDStV C': 10};
    expect(bm.vorschlaege(50.7753, 6.0839).first.$1.name, 'KDStV C');
  });

  test('Filter übersteht JSON', () {
    final f = BummelFilter(gast: 'ADB', reinMin: 40, vorbeiMin: 3, unbekannt: false)..bereiche[Skala.lk] = const RangeValues(2, 7);
    final g = BummelFilter.json(f.json());
    expect(g.gast, 'ADB');
    expect(g.reinMin, 40);
    expect(g.vorbeiMin, 3);
    expect(g.unbekannt, false);
    expect(g.bereiche[Skala.lk], const RangeValues(2, 7));
    expect(g.bereiche[Skala.lr], const RangeValues(1, 10));
  });

  test('Zurück: nur Häuser, die näher ans Ende bringen; Bund am Ende fällt raus', () {
    bm.zuhause = Ort('adresse', 'Zuhause', '', 50.790, 6.084); // nördlich
    bm.zurueckPhase = true;
    expect(bm.vorschlaege(50.7753, 6.0839).length, 3);
    bm.zuhause = Ort('adresse', 'Zuhause', '', 50.760, 6.084); // südlich: alle Häuser führen weg
    expect(bm.vorschlaege(50.7753, 6.0839), isEmpty);
    bm.zurueckPhase = false;
    expect(bm.vorschlaege(50.7753, 6.0839), isNotEmpty); // auf dem Hinweg kostet die Entfernung zum Ende nur etwas
    bm.filter = BummelFilter(endeBund: 'Corps B');
    expect(bm.endeOrt?.name, 'Corps B');
    expect(bm.vorschlaege(50.7753, 6.0839).map((x) => x.$1.name), isNot(contains('Corps B')));
    expect(BummelFilter.json(bm.filter!.json()).endeBund, 'Corps B');
  });
}
