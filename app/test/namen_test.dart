import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/namen.dart';

void main() {
  test('Haltnamen ohne Richtung und Bahnsteig', () {
    const faelle = {
      'Würselen Kaninsberg lw -> Haaren': 'Würselen Kaninsberg',
      'Würselen Kaninsberg sw': 'Würselen Kaninsberg',
      'Aachen Westbahnhof lw (Li.33) H2': 'Aachen Westbahnhof',
      'Tannenallee sw ( -> LICH)': 'Tannenallee',
      'Breinig Denkmal lw -> Li.35 ENTE': 'Breinig Denkmal',
      'Aachen Bushof H13': 'Aachen Bushof',
      'Jülich Bahnhof/ZOB D': 'Jülich Bahnhof/ZOB',
      'Elisenbrunnen H2': 'Elisenbrunnen',
      'Aachen Hbf': 'Aachen Hbf',
      'Paustenbach Am Venn sw': 'Paustenbach Am Venn',
    };
    faelle.forEach((roh, soll) => expect(haltName(roh), soll, reason: roh));
  });

  test('Ziele ohne Kürzel, Ortszusatz bleibt', () {
    expect(zielName('Fuchserde (Waldfriedhof Nord) lw (33)'), 'Fuchserde (Waldfriedhof Nord)');
    expect(zielName('Aachen Diepenbenden sw -> Wendeschleife'), 'Aachen Diepenbenden');
    expect(zielName('Vaals Grenze lw (Püngelerstr.)'), 'Vaals Grenze (Püngelerstr.)');
  });

  test('Hbf und Hauptbahnhof sind ein Ort', () {
    expect(haltSchluessel('Aachen Hauptbahnhof H1'), haltSchluessel('Aachen Hbf'));
  });

  test('Bahnsteig aus dem Rohnamen', () {
    expect(bahnsteig('Aachen Hauptbahnhof H1'), '1');
    expect(bahnsteig('Aachen Westbahnhof lw (Li.33) H2'), '2');
    expect(bahnsteig('Aachen Bushof B'), 'B');
    expect(bahnsteig('Aachen Hbf'), isNull);
    expect(bahnsteig('Würselen Kaninsberg lw -> Haaren'), isNull);
  });
}
