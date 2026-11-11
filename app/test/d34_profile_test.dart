// Unabhängige Prüfung D-34: TempoSeite/ProfilSeite und Umstellung alter tempo.json, mit echter Datei im Temp-Ordner.
// Der Abschnitt „FUND“ hält Fehler aus der Prüfung von 6aa343b fest, die seitdem behoben sind.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/tempo.dart';
import 'package:schnellweg/tempo_messen.dart';

late Directory _ordner;
File get _datei => File('${_ordner.path}/tempo.json');
Map<String, dynamic> _inhalt() => jsonDecode(_datei.readAsStringSync()) as Map<String, dynamic>;
Map<String, dynamic> _profil(String k) => (_inhalt()['profile'] as Map<String, dynamic>)[k] as Map<String, dynamic>;
void _schreiben(Object j) => _datei.writeAsStringSync(j is String ? j : jsonEncode(j));

Map<String, dynamic> _v2([Map<String, dynamic> extra = const {}, String aktiv = 'entspannt']) => {
      'v': 2,
      'aktiv': aktiv,
      'profile': {
        'entspannt': {'art': 'fuss', 'kmh': 4.3},
        'Auto': {'art': 'auto', 'kmh': 130.0},
        ...extra,
      },
    };

const _gemessen = '2026-10-28T10:00:00.000';

/// Echte Datei-Ein-/Ausgabe läuft in testWidgets nur in runAsync.
Future<void> _warten(WidgetTester t) async {
  // Solange die Datei gelesen wird, dreht sich der Ladekreis; pumpAndSettle käme damit nie zur Ruhe.
  for (var i = 0; i < 200; i++) {
    await t.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
    await t.pump();
    // Auf langsamen Rechnern (CI) ist die Datei nach ein paar Takten noch nicht geschrieben.
    if (i >= 4 && Tempo.speichertGerade == 0 && find.byType(CircularProgressIndicator).evaluate().isEmpty) break;
  }
  await t.pumpAndSettle();
}

Future<void> _seite(WidgetTester t) async {
  t.view.physicalSize = const Size(1000, 6000);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.reset);
  await t.pumpWidget(const MaterialApp(home: TempoSeite()));
  await _warten(t);
}

Future<void> _menue(WidgetTester t, String profil, String punkt) async {
  await t.tap(find.descendant(of: find.widgetWithText(ListTile, profil), matching: find.byType(PopupMenuButton<String>)));
  await t.pumpAndSettle();
  await t.tap(find.text(punkt));
  await t.pumpAndSettle();
}

Finder get _uebernehmen => find.widgetWithText(TextButton, 'Übernehmen');
bool _kannUebernehmen(WidgetTester t) => t.widget<TextButton>(_uebernehmen).onPressed != null;

Future<void> _speichernTippen(WidgetTester t) async {
  await t.tap(_uebernehmen);
  await t.pumpAndSettle();
  await _warten(t);
}

Finder _feld(String label) => find.widgetWithText(TextField, label);

/// Regler in der Zeile mit diesem Text.
Finder _regler(String text) => find.descendant(of: find.ancestor(of: find.text(text), matching: find.byType(Row)).first, matching: find.byType(Slider));

Future<void> _ganzLinks(WidgetTester t, Finder f) async {
  await t.drag(f, const Offset(-3000, 0));
  await t.pumpAndSettle();
}

Future<void> _ganzRechts(WidgetTester t, Finder f) async {
  await t.drag(f, const Offset(3000, 0));
  await t.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    _ordner = Directory.systemTemp.createTempSync('d34');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(
        const MethodChannel('plugins.flutter.io/path_provider'), (_) async => _ordner.path);
  });
  tearDown(() => _ordner.deleteSync(recursive: true));

  group('Seite: anlegen', () {
    testWidgets('neues Profil landet mit Name, Tempo und v:2 in der Datei', (t) async {
      _schreiben(_v2());
      await _seite(t);
      await t.tap(find.text('Profil'));
      await t.pumpAndSettle();
      expect(_kannUebernehmen(t), isFalse, reason: 'ohne Name');
      await t.enterText(_feld('Name'), '   ');
      await t.pump();
      expect(_kannUebernehmen(t), isFalse, reason: 'nur Leerzeichen');
      await t.enterText(_feld('Name'), '  Inliner ');
      await t.enterText(_feld('Tempo'), '11,5');
      await t.pump();
      await _speichernTippen(t);
      expect(find.text('Inliner'), findsOneWidget);
      final j = _inhalt();
      expect(j['v'], 2);
      expect(j['aktiv'], 'entspannt');
      final p = (j['profile'] as Map).values.last as Map;
      expect((p['art'], p['kmh'], p['titel']), ('eigen', 11.5, 'Inliner'));
      expect(p.containsKey('meiden'), isFalse);
    });

    testWidgets('Zurück beim Anlegen legt nichts an', (t) async {
      _schreiben(_v2());
      final vorher = _datei.readAsStringSync();
      await _seite(t);
      await t.tap(find.text('Profil'));
      await t.pumpAndSettle();
      await t.enterText(_feld('Name'), 'Weg damit');
      await t.tap(find.byType(BackButton));
      await t.pumpAndSettle();
      await _warten(t);
      expect(find.text('Weg damit'), findsNothing);
      expect(_datei.readAsStringSync(), vorher);
    });
  });

  group('Seite: bearbeiten', () {
    final mofa = {
      'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa', 'gemessen': _gemessen, 'meiden': {'treppen': 0.5}, 'ticket': true, 'fern': true}
    };

    testWidgets('nur Name ändern behält Tempo, Messung und Auswahl', (t) async {
      _schreiben(_v2(mofa, 'p1'));
      await _seite(t);
      await _menue(t, 'Mofa', 'Bearbeiten');
      await t.enterText(_feld('Name'), 'Moped');
      await t.pump();
      await _speichernTippen(t);
      final p = _profil('p1');
      expect((p['titel'], p['kmh'], p['gemessen'], p['ticket'], p['fern']), ('Moped', 40.0, _gemessen, true, true));
      expect(p['meiden'], {'treppen': 0.5});
      expect(_inhalt()['aktiv'], 'p1');
    });

    testWidgets('Tempo ändern löscht die Messung', (t) async {
      _schreiben(_v2(mofa));
      await _seite(t);
      await _menue(t, 'Mofa', 'Bearbeiten');
      await t.enterText(_feld('Tempo'), '45');
      await t.pump();
      await _speichernTippen(t);
      expect((_profil('p1')['kmh'], _profil('p1').containsKey('gemessen')), (45.0, false));
    });

    testWidgets('Zurück verwirft alles, Datei und Liste bleiben', (t) async {
      _schreiben(_v2(mofa));
      final vorher = _datei.readAsStringSync();
      await _seite(t);
      await _menue(t, 'Mofa', 'Bearbeiten');
      await t.enterText(_feld('Name'), 'Anders');
      await t.enterText(_feld('Tempo'), '1');
      await _ganzLinks(t, _regler('Treppen'));
      await t.tap(find.text('Fußwege'));
      await t.pumpAndSettle();
      await t.tap(find.byType(BackButton));
      await t.pumpAndSettle();
      await _warten(t);
      expect(find.text('Mofa'), findsOneWidget);
      expect(_datei.readAsStringSync(), vorher);
      // und nochmal öffnen: die Seite zeigt den alten Stand, nicht die verworfene Kopie
      await _menue(t, 'Mofa', 'Bearbeiten');
      expect(t.widget<TextField>(_feld('Tempo')).controller!.text, '40,0');
      expect(t.widget<Slider>(_regler('Treppen')).value, 0.5);
    });

    testWidgets('Auto: kein Name, keine ÖV-Schalter; Meiden kommt in die Datei', (t) async {
      _schreiben(_v2());
      await _seite(t);
      await _menue(t, 'Auto', 'Bearbeiten');
      expect(_feld('Name'), findsNothing);
      expect(find.text('Bus und Bahn'), findsNothing);
      await _ganzRechts(t, _regler('Autobahn'));
      await _speichernTippen(t);
      expect(_profil('Auto')['art'], 'auto');
      expect(_profil('Auto')['meiden'], {'autobahn': 1.0});
    });
  });

  group('Seite: löschen', () {
    final eins = {
      'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa'}
    };

    testWidgets('aktives Profil löschen: zurück auf Zu Fuß, in Datei und Liste', (t) async {
      _schreiben(_v2(eins, 'p1'));
      await _seite(t);
      await _menue(t, 'Mofa', 'Löschen');
      expect(find.text('„Mofa“ löschen?'), findsOneWidget);
      await t.tap(find.widgetWithText(FilledButton, 'Löschen'));
      await t.pumpAndSettle();
      await _warten(t);
      expect(find.text('Mofa'), findsNothing);
      expect(_inhalt()['aktiv'], 'entspannt');
      expect((_inhalt()['profile'] as Map).containsKey('p1'), isFalse);
      expect((await t.runAsync(Tempo.laden))!.name, 'entspannt');
    });

    testWidgets('Abbrechen im Dialog löscht nichts', (t) async {
      _schreiben(_v2(eins, 'p1'));
      final vorher = _datei.readAsStringSync();
      await _seite(t);
      await _menue(t, 'Mofa', 'Löschen');
      await t.tap(find.widgetWithText(TextButton, 'Abbrechen'));
      await t.pumpAndSettle();
      await _warten(t);
      expect(_datei.readAsStringSync(), vorher);
    });

    testWidgets('Zu Fuß und Auto lassen sich nicht löschen', (t) async {
      _schreiben(_v2());
      await _seite(t);
      for (final n in ['Zu Fuß', 'Auto']) {
        await t.tap(find.descendant(of: find.widgetWithText(ListTile, n), matching: find.byType(PopupMenuButton<String>)));
        await t.pumpAndSettle();
        expect(find.text('Löschen'), findsNothing);
        await t.tapAt(const Offset(5, 5));
        await t.pumpAndSettle();
      }
    });

    testWidgets('Antippen macht aktiv und speichert', (t) async {
      _schreiben(_v2(eins));
      await _seite(t);
      await t.tap(find.text('Mofa'));
      await _warten(t);
      expect(_inhalt()['aktiv'], 'p1');
    });
  });

  group('Seite: Eingabeprüfung', () {
    Future<void> pruefen(WidgetTester t, String profil, Map<String, bool> faelle) async {
      await _seite(t);
      await _menue(t, profil, 'Bearbeiten');
      for (final MapEntry(key: ein, value: ok) in faelle.entries) {
        await t.enterText(_feld(profil == 'Auto' ? 'Höchsttempo' : 'Tempo'), ein);
        await t.pump();
        expect(_kannUebernehmen(t), ok, reason: '„$ein“ bei $profil');
      }
    }

    testWidgets('zu Fuß 2–8', (t) async {
      _schreiben(_v2());
      await pruefen(t, 'Zu Fuß', {'1,9': false, '2': true, '5,5': true, '5.5': true, '8': true, '8,1': false, '0': false, '-3': false, '': false, 'abc': false, ',': false, '1,2,3': false, 'NaN': false});
    });

    testWidgets('Auto 30–500', (t) async {
      _schreiben(_v2());
      await pruefen(t, 'Auto', {'29,9': false, '30': true, '350': true, '500': true, '500,1': false, '0': false});
    });

    testWidgets('eigenes Profil: alles über 0', (t) async {
      _schreiben(_v2({
        'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa'}
      }));
      await pruefen(t, 'Mofa', {'0': false, '0,0': false, '-1': false, '0,1': true, '999': true, 'abc': false});
    });
  });

  group('Seite: Schalter und Regler', () {
    Future<void> oeffnen(WidgetTester t, [Map<String, dynamic> m = const {}]) async {
      _schreiben(_v2({
        'p1': {'art': 'eigen', 'kmh': 20.0, 'titel': 'Rad', if (m.isNotEmpty) 'meiden': m}
      }));
      await _seite(t);
      await _menue(t, 'Rad', 'Bearbeiten');
    }

    testWidgets('Regler auf 0 fällt aus meiden raus', (t) async {
      await oeffnen(t, {'treppen': 0.5, 'steigung': 0.3});
      await _ganzLinks(t, _regler('Treppen'));
      await _speichernTippen(t);
      expect(_profil('p1')['meiden'], {'steigung': 0.3});
    });

    testWidgets('letzter Regler auf 0: gar kein meiden in der Datei', (t) async {
      await oeffnen(t, {'treppen': 0.5});
      await _ganzLinks(t, _regler('Treppen'));
      await _speichernTippen(t);
      expect(_profil('p1').containsKey('meiden'), isFalse);
    });

    testWidgets('Regler ganz rechts = 1,0; Zwischenwerte bleiben in 0–1', (t) async {
      await oeffnen(t);
      await _ganzRechts(t, _regler('Fähren'));
      await t.drag(_regler('Treppen'), const Offset(60, 0));
      await t.pumpAndSettle();
      await _speichernTippen(t);
      final m = _profil('p1')['meiden'] as Map;
      expect(m['faehren'], 1.0);
      expect(m['treppen'], allOf(greaterThan(0), lessThanOrEqualTo(1)));
    });

    testWidgets('Anlieger-Zufahrten: Schalter landet in meiden', (t) async {
      await oeffnen(t);
      await t.scrollUntilVisible(find.text('Anlieger-Zufahrten mitnehmen'), 100, scrollable: find.byType(Scrollable).last);
      await t.tap(find.text('Anlieger-Zufahrten mitnehmen'));
      await t.pumpAndSettle();
      await _speichernTippen(t);
      expect(_profil('p1')['meiden'], {'anlieger': true});
    });

    testWidgets('Fußwege blendet Hauptstraßen ein und Unbeleuchtet aus; Schalter aus fällt raus', (t) async {
      await oeffnen(t);
      expect(find.text('Hauptstraßen'), findsNothing);
      expect(find.text('Unbeleuchtete Wege'), findsOneWidget);
      await t.tap(find.text('Fußwege'));
      await t.pumpAndSettle();
      expect(find.text('Hauptstraßen'), findsOneWidget);
      expect(find.text('Unbeleuchtete Wege'), findsNothing);
      await t.tap(find.text('Fußwege'));
      await t.pumpAndSettle();
      await _speichernTippen(t);
      expect(_profil('p1').containsKey('meiden'), isFalse);
    });

    testWidgets('Bus und Bahn meiden blendet den ÖV-Teil aus', (t) async {
      await oeffnen(t);
      await t.tap(find.text('Bus und Bahn ganz meiden'));
      await t.pumpAndSettle();
      for (final x in ['Busse meiden', 'Umstiege', 'Fernverkehr meiden', 'Fahrradticket nötig']) {
        expect(find.text(x), findsNothing, reason: x);
      }
      await _speichernTippen(t);
      expect(_profil('p1')['meiden'], {'oev': true});
    });

    testWidgets('Bus-Minuten: 0 fällt raus, Wert ist eine ganze Zahl', (t) async {
      await oeffnen(t, {'bus_min': 5});
      expect(find.text('Bus nur, wenn er 5 min spart'), findsOneWidget);
      await _ganzRechts(t, _regler('Bus nur, wenn er 5 min spart'));
      expect(find.text('Bus nur, wenn er 30 min spart'), findsOneWidget);
      await _ganzLinks(t, _regler('Bus nur, wenn er 30 min spart'));
      expect(find.text('Bus muss nichts sparen'), findsOneWidget);
      await _speichernTippen(t);
      expect(_profil('p1').containsKey('meiden'), isFalse);
    });

    testWidgets('Umstiege: höchstens 0 bleibt, egal fällt raus', (t) async {
      await oeffnen(t);
      await t.tap(find.byType(DropdownButton<int?>));
      await t.pumpAndSettle();
      await t.tap(find.text('höchstens 0').last);
      await t.pumpAndSettle();
      await _speichernTippen(t);
      expect(_profil('p1')['meiden'], {'umstiege': 0});
      await _menue(t, 'Rad', 'Bearbeiten');
      await t.tap(find.byType(DropdownButton<int?>));
      await t.pumpAndSettle();
      await t.tap(find.text('egal').last);
      await t.pumpAndSettle();
      await _speichernTippen(t);
      expect(_profil('p1').containsKey('meiden'), isFalse);
    });

    testWidgets('Fernverkehr und Ticket landen im Profil, nicht in meiden', (t) async {
      await oeffnen(t);
      await t.tap(find.text('Fernverkehr meiden'));
      await t.pumpAndSettle();
      await t.tap(find.text('Fahrradticket nötig'));
      await t.pumpAndSettle();
      await _speichernTippen(t);
      final p = _profil('p1');
      expect((p['fern'], p['ticket'], p.containsKey('meiden')), (true, true, false));
    });

    testWidgets('zu Fuß: nur die passenden Stufen', (t) async {
      _schreiben(_v2());
      await _seite(t);
      await _menue(t, 'Zu Fuß', 'Bearbeiten');
      for (final x in ['Treppen', 'Unbeleuchtete Wege', 'Fähren']) {
        expect(find.text(x), findsOneWidget, reason: x);
      }
      for (final x in ['Fußwege', 'Hauptstraßen', 'Autobahn', 'Mautstraßen', 'Fahrradticket nötig']) {
        expect(find.text(x), findsNothing, reason: x);
      }
    });
  });

  // Stände aus der Git-Geschichte von tempo.dart: toJson schrieb kmh, gemessen, titel; aktiv oben.
  group('Umstellung auf v2', () {
    Future<Tempo> offen() => Tempo.oeffnen();

    test('c4deb9e: Fahrrad wird Fixie, Messung und Auswahl bleiben', () async {
      _schreiben({
        'aktiv': 'Fahrrad schnell',
        'profile': {
          'entspannt': {'kmh': 4.4},
          'normal': {'kmh': 5.0},
          'schnell': {'kmh': 6.0},
          'Einrad': {'kmh': 11.0, 'gemessen': _gemessen},
          'Einrad schnell': {'kmh': 15.0},
          'Fahrrad': {'kmh': 17.0, 'gemessen': _gemessen},
          'Fahrrad schnell': {'kmh': 23.5, 'gemessen': _gemessen},
        },
      });
      final t = await offen();
      expect((t.aktiv, t.aktuell.kmh, t.aktuell.gemessen != null, t.aktuell.ticket), ('Fixie schnell', 23.5, true, true));
      final f = t.profile.firstWhere((p) => p.name == 'Fixie');
      expect((f.kmh, f.gemessen != null), (17.0, true));
      expect(t.profile.firstWhere((p) => p.name == 'Einrad').gemessen, isNotNull);
      expect(t.profile.first.kmh, 4.4);
      expect(_inhalt()['v'], 2);
    });

    test('Fahrrad und Fixie zugleich: Fixie gewinnt', () async {
      _schreiben({
        'aktiv': 'Fixie',
        'profile': {
          'Fahrrad': {'kmh': 17.0},
          'Fixie': {'kmh': 22.0},
        },
      });
      final t = await offen();
      expect((t.aktiv, t.aktuell.kmh), ('Fixie', 22.0));
    });

    test('voller Stand mit allen 15 Profilen je aktiv: nichts geht verloren', () async {
      const namen = ['Einrad', 'Einrad schnell', 'Klapprad entspannt', 'Klapprad', 'Klapprad schnell', 'Fixie entspannt', 'Fixie', 'Fixie schnell', 'Rennrad entspannt', 'Rennrad', 'Rennrad schnell', 'Mountainbike entspannt', 'Mountainbike', 'Mountainbike schnell', 'Auto'];
      for (final aktiv in [...namen, 'entspannt', 'normal', 'schnell']) {
        final alt = <String, dynamic>{
          'entspannt': {'kmh': 4.7, 'gemessen': _gemessen},
          'normal': {'kmh': 5.3, 'gemessen': _gemessen},
          'schnell': {'kmh': 6.3},
          for (final (i, n) in namen.indexed) n: {'kmh': 31.0 + i, 'gemessen': _gemessen, 'titel': 'T$i'},
        };
        _schreiben({'aktiv': aktiv, 'profile': alt});
        final t = await offen();
        expect(t.aktiv, aktiv == 'normal' || aktiv == 'schnell' ? 'entspannt' : aktiv);
        for (final (i, n) in namen.indexed) {
          final p = t.profile.firstWhere((p) => p.name == n);
          expect(p.kmh, 31.0 + i, reason: n);
          expect(p.gemessen, DateTime.parse(_gemessen), reason: n);
          if (n != 'Auto') expect(p.anzeige, 'T$i', reason: n);
        }
        final f = t.profile.firstWhere((p) => p.art == 'fuss');
        expect(f.kmh, aktiv == 'normal' ? 5.3 : aktiv == 'schnell' ? 6.3 : 4.7);
        expect(t.profile.length, 16);
      }
    });

    test('zweites Öffnen ändert kein Byte', () async {
      _schreiben({
        'aktiv': 'normal',
        'profile': {
          'normal': {'kmh': 5.3},
          'Rennrad': {'kmh': 28.0, 'gemessen': _gemessen},
        },
      });
      await offen();
      final erst = _datei.readAsStringSync();
      final t = await offen();
      await offen();
      expect(_datei.readAsStringSync(), erst);
      expect((t.aktiv, t.aktuell.kmh), ('entspannt', 5.3));
    });

    test('v2 wird beim Öffnen nicht neu geschrieben, Kachel-Zahlen ohne .0 gehen', () async {
      // Android-JSONObject schreibt 15.0 als 15 und 1.0 als 1
      const roh = '{"v":2,"aktiv":"p1","profile":{"entspannt":{"art":"fuss","kmh":4},"Auto":{"art":"auto","kmh":130},'
          '"p1":{"art":"eigen","kmh":15,"titel":"Rad","meiden":{"treppen":1,"bus_min":5,"umstiege":0}}}}';
      _schreiben(roh);
      final t = await offen();
      expect(_datei.readAsStringSync(), roh);
      expect((t.aktuell.kmh, t.aktuell.stufe('treppen')), (15.0, 1.0));
      expect(t.profile.first.kmh, 4.0);
    });

    test('Kachel hat vor der Umstellung nur aktiv geändert', () async {
      _schreiben('{"aktiv":"Klapprad","profile":{"Klapprad":{"kmh":14.5,"gemessen":"$_gemessen"}}}');
      final t = await offen();
      expect((t.aktiv, t.aktuell.kmh, t.aktuell.meiden['bus_min']), ('Klapprad', 14.5, 5));
    });

    test('ganz alte Datei „tempo“ (Einzelwert) wird Gehtempo und verschwindet', () async {
      final alt = File('${_ordner.path}/tempo')..writeAsStringSync('5.5');
      final t = await offen();
      expect((t.aktiv, t.aktuell.kmh, alt.existsSync()), ('entspannt', 5.5, false));
      expect(_inhalt()['v'], 2);
      expect(_profil('entspannt')['kmh'], 5.5);
    });

    test('Neuinstallation: nur Zu Fuß und Auto, nichts geschrieben', () async {
      final t = await offen();
      expect(t.profile.map((p) => p.anzeige), ['Zu Fuß', 'Auto']);
      expect(_datei.existsSync(), isFalse);
    });

    test('aktiv zeigt auf ein unbekanntes Profil: fällt auf Zu Fuß', () async {
      _schreiben(_v2({}, 'gibtsnicht'));
      expect((await offen()).aktuell.name, 'entspannt');
    });

    test('kaputte Datei, die kein JSON ist: Vorgaben, Datei bleibt liegen', () async {
      for (final roh in ['', '{', '{"v":2,"aktiv":"p1","profile":{"entspannt":{"art":"fuss"']) {
        _schreiben(roh);
        final t = await offen();
        expect(t.aktuell.name, 'entspannt');
        expect(_datei.readAsStringSync(), roh);
      }
    });
  });

  group('FUND', () {
    // Umgestellte Profile haben keinen titel; das Namensfeld ist dann leer und Übernehmen gesperrt.
    testWidgets('umgestelltes Profil lässt sich bearbeiten, ohne den Namen neu zu tippen', (t) async {
      _schreiben({
        'aktiv': 'entspannt',
        'profile': {
          'Rennrad': {'kmh': 28.0}
        }
      });
      await _seite(t);
      await _menue(t, 'Rennrad', 'Bearbeiten');
      expect(t.widget<TextField>(_feld('Name')).controller!.text, 'Rennrad');
      expect(_kannUebernehmen(t), isTrue);
    });

    // Nur der Name war nie geändert: Nach dem Bearbeiten darf der angezeigte Name nicht anders sein.
    testWidgets('Custom 1 (umgestellt) behält den Namen im Feld', (t) async {
      _schreiben({
        'aktiv': 'entspannt',
        'profile': {
          'Einrad': {'kmh': 12.0}
        }
      });
      await _seite(t);
      await _menue(t, 'Custom 1', 'Bearbeiten');
      expect(t.widget<TextField>(_feld('Name')).controller!.text, 'Custom 1');
    });

    // double.tryParse nimmt „Infinity“ und „1e999“; erlaubt() ebenso, danach wirft jsonEncode.
    testWidgets('Tempo unendlich wird nicht angenommen', (t) async {
      _schreiben(_v2({
        'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa'}
      }));
      await _seite(t);
      await _menue(t, 'Mofa', 'Bearbeiten');
      for (final x in ['Infinity', '1e999']) {
        await t.enterText(_feld('Tempo'), x);
        await t.pump();
        expect(_kannUebernehmen(t), isFalse, reason: x);
      }
    });

    // Der Server lehnt BaseKMH/kmh >= 1e4 mit 400 ab (api.go walkScale), die App lässt es durch.
    testWidgets('Tempo, das der Server ablehnt, wird nicht angenommen', (t) async {
      _schreiben(_v2({
        'p1': {'art': 'eigen', 'kmh': 40.0, 'titel': 'Mofa'}
      }));
      await _seite(t);
      await _menue(t, 'Mofa', 'Bearbeiten');
      await t.enterText(_feld('Tempo'), '0,0001');
      await t.pump();
      expect(_kannUebernehmen(t), isFalse);
    });

    // Kein Fall darf eine Ausnahme werfen: _starten() wartet auf Tempo.laden, sonst bleibt die App hängen.
    for (final (name, roh) in [
      ('JSON-Liste', '[]'),
      ('null', 'null'),
      ('Zahl', '42'),
      ('leeres Objekt', '{}'),
      ('ohne profile', '{"aktiv":"entspannt"}'),
      ('profile keine Map', '{"profile":[]}'),
      ('alt, kmh als Text', '{"aktiv":"Einrad","profile":{"Einrad":{"kmh":"12"}}}'),
      ('alt, Profil keine Map', '{"aktiv":"Einrad","profile":{"Einrad":12}}'),
      ('v2, ohne art', '{"v":2,"aktiv":"p1","profile":{"p1":{"kmh":12}}}'),
      ('v2, ohne kmh', '{"v":2,"aktiv":"p1","profile":{"p1":{"art":"eigen"}}}'),
      ('v2, unbekannte art', '{"v":2,"aktiv":"p1","profile":{"p1":{"art":"rakete","kmh":12}}}'),
      ('v2, meiden keine Map', '{"v":2,"aktiv":"p1","profile":{"p1":{"art":"eigen","kmh":12,"meiden":[]}}}'),
    ]) {
      test('kaputte Datei wirft nicht: $name', () async {
        _schreiben(roh);
        final t = await Tempo.oeffnen();
        expect(t.aktuell.min, isNotNull); // unbekannte art: grenzen[art]! wirft sonst erst in der Oberfläche
      });
    }

    test('v2 mit einem kaputten Profil behält die anderen', () async {
      _schreiben('{"v":2,"aktiv":"gut","profile":{"gut":{"art":"eigen","kmh":12,"titel":"Gut"},"p1":{"art":"eigen"}}}');
      final t = await Tempo.oeffnen();
      expect(t.aktuell.titel, 'Gut');
    });

    test('neuere Datei (v:3) wird nicht als alter Stand überschrieben', () async {
      const roh = '{"v":3,"aktiv":"p1","profile":{"entspannt":{"art":"fuss","kmh":4.3},"p1":{"art":"eigen","kmh":12,"titel":"Gut"}}}';
      _schreiben(roh);
      await Tempo.oeffnen();
      expect(_datei.readAsStringSync(), roh);
    });

    test('leerer Titel aus einer alten Datei ergibt keinen leeren Listeneintrag', () async {
      _schreiben({
        'aktiv': 'Einrad',
        'profile': {
          'Einrad': {'kmh': 12.0, 'titel': ''}
        }
      });
      expect((await Tempo.oeffnen()).aktuell.anzeige, isNotEmpty);
    });

    // Eine kaputte Datei ergibt die Vorgaben; das nächste Speichern überschreibt dann alle Profile endgültig.
    test('kaputte Datei geht beim nächsten Speichern nicht verloren', () async {
      const roh = '{"v":2,"aktiv":"p1","profile":{"p1":{"art":"eigen","kmh":12,"titel":"Mein Rad"';
      _schreiben(roh);
      final t = await Tempo.oeffnen();
      t.aktiv = 'Auto';
      await t.speichern();
      final da = _ordner.listSync().whereType<File>().any((f) => f.readAsStringSync() == roh);
      expect(da, isTrue, reason: 'keine Sicherung der kaputten Datei');
    });

    // speichern() kürzt die Datei und schreibt dann: wer dazwischen liest, sieht eine leere Datei. Wettlauf: je nach Lauf.
    test('Lesen während des Speicherns sieht nie die Vorgaben', () async {
      _schreiben(_v2({
        'p1': {'art': 'eigen', 'kmh': 12.0, 'titel': 'Gut'}
      }, 'p1'));
      final t = await Tempo.oeffnen();
      var falsch = 0;
      for (var i = 0; i < 300; i++) {
        final s = t.speichern();
        final r = await Tempo.oeffnen();
        if (r.aktiv != 'p1') falsch++;
        await s;
      }
      expect(falsch, 0);
    });

    // Erzwingt zwei Schreiber zugleich (App und Kachel, schnelle Taps). Wettlauf: schlägt je nach Lauf fehl.
    test('zwei Speichern kurz nacheinander ergeben gültiges JSON', () async {
      _schreiben(_v2());
      final lang = await Tempo.oeffnen();
      lang.profile.add(TempoProfil('lang', 'eigen', 10)..titel = 'x' * 4000);
      final kurz = await Tempo.oeffnen();
      var kaputt = 0;
      for (var i = 0; i < 300; i++) {
        await Future.wait([lang.speichern(), kurz.speichern()]);
        try {
          jsonDecode(_datei.readAsStringSync());
        } on FormatException {
          kaputt++;
        }
      }
      expect(kaputt, 0);
    });

    // Verlässt man die Seite, bevor die Datei gelesen ist, ruft die Seite setState nach dispose auf.
    testWidgets('Seite sofort wieder verlassen wirft nicht', (t) async {
      _schreiben(_v2());
      await t.pumpWidget(const MaterialApp(home: TempoSeite()));
      await t.pumpWidget(const MaterialApp(home: SizedBox()));
      await _warten(t);
      expect(t.takeException(), isNull);
    });
  });
}
