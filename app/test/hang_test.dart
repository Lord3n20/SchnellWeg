import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/tempo.dart';

void main() {
  test('Regler gehen als km/h an den Server und folgen dem Tempo', () {
    final p = TempoProfil('Longboard', 'eigen', 12)
      ..auf = -50
      ..ab = 100;
    expect(p.serverMeiden, {'kmh_auf': 6.0, 'kmh_ab': 24.0});
    p.kmh = 10;
    expect(p.serverMeiden, {'kmh_auf': 5.0, 'kmh_ab': 20.0});
    expect(p.meiden, isEmpty);
  });

  test('Zu Fuß mit Hang schickt meiden statt art, ohne Hang wie bisher', () {
    final p = TempoProfil('entspannt', 'fuss', 4.3);
    expect(p.serverArt, 'fuss');
    expect(p.serverMeiden, isNull);
    p.ab = 20;
    expect(p.serverArt, isNull);
    expect(p.serverMeiden!.keys, ['kmh_ab']);
  });

  test('Über 200 km/h nimmt der Server nicht an', () {
    final p = TempoProfil('x', 'eigen', 150)..ab = 200;
    expect(p.serverMeiden!['kmh_ab'], 200.0);
  });

  test('km/h aus 0.2.4 werden zu Prozent und verlassen meiden', () {
    final p = TempoProfil.json('x', {
      'art': 'eigen',
      'kmh': 10,
      'meiden': {'pflaster': true, 'kmh_auf': 7, 'kmh_ab': 18}
    });
    expect((p.auf, p.ab), (-30, 80));
    expect(p.meiden, {'belag': 0.8});
    expect(p.toJson()['auf'], -30);
    final q = TempoProfil.json('x', p.toJson());
    expect((q.auf, q.ab), (-30, 80));
  });

  test('Zu Fuß ab: bergauf Gehtempo bei der Steigung, ersetzt den Prozentregler', () {
    final p = TempoProfil('Longboard', 'eigen', 12)
      ..auf = -50
      ..zuFussAb = 4;
    expect(p.serverMeiden, {'kmh_auf': standardKmh, 'hang_voll': 4});
    final q = TempoProfil.json('x', p.toJson());
    expect(q.zuFussAb, 4);
  });

  test('Schalter aus 0.2.4 bis 0.2.6 wird zur Belag-Stufe', () {
    final p = TempoProfil.json('x', {
      'art': 'eigen',
      'kmh': 10,
      'meiden': {'pflaster': true}
    });
    expect(p.meiden, {'belag': 0.8});
    final q = TempoProfil.json('x', {
      'art': 'eigen',
      'kmh': 10,
      'meiden': {'pflaster': true, 'belag': 1.0}
    });
    expect(q.meiden, {'belag': 1.0});
  });

  test('Auto schickt keinen Hang', () {
    final p = TempoProfil('Auto', 'auto', 120)..ab = 50;
    expect(p.serverMeiden, isNull);
    expect(p.serverArt, 'auto');
  });
}
