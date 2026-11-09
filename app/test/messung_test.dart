import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:schnellweg/messung.dart';
import 'package:schnellweg/standort.dart';

void main() {
  test('Zeilen werden erkannt, Unlesbares übersprungen', () {
    final f = messZeile('f,50.77,6.08,4.5,1.234567E8,3.25') as Fix;
    expect([f.lat, f.lon, f.genau, f.zeit, f.tempo], [50.77, 6.08, 4.5, 123456700.0, 3.25]);
    expect((messZeile('p') as MessPause).an, isTrue);
    expect((messZeile('w') as MessPause).an, isFalse);
    expect(messZeile('k,longboard'), isNull);
    expect(messZeile('f,50.77,abc,4.5,1,1'), isNull);
    expect(messZeile('f,50.77'), isNull);
  });

  test('Leser liefert nur Neues und wartet auf vollständige Zeilen', () async {
    final ordner = await Directory.systemTemp.createTemp('messung');
    addTearDown(() => ordner.delete(recursive: true));
    final d = File('${ordner.path}/m.txt');
    final l = MessLeser(d);
    expect(await l.neue(), isEmpty);
    await d.writeAsString('k,x\nw\nf,1,2,3,4,5\nf,1,2');
    expect((await l.neue()).length, 2);
    expect(await l.neue(), isEmpty);
    await d.writeAsString(',3,9,5\np\n', mode: FileMode.append);
    final neu = await l.neue();
    expect((neu[0] as Fix).zeit, 9);
    expect(neu[1], isA<MessPause>());
  });
}
