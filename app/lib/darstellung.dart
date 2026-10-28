// Darstellung: hell/dunkel nach System oder fest, im Dunkelmodus wahlweise OLED-Schwarz (echtes Schwarz spart auf
// OLED-Displays Strom). Nur auf dem Handy (darstellung.json im App-Ordner); gilt sofort für App und Karte.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

class Darstellung extends ChangeNotifier {
  Darstellung._();
  static final instanz = Darstellung._();

  ThemeMode modus = ThemeMode.system;
  bool oled = false;

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/darstellung.json');

  /// Vor runApp, damit die App nicht erst hell aufblitzt.
  Future<void> laden() async {
    try {
      final j = jsonDecode(await (await _datei()).readAsString()) as Map<String, dynamic>;
      modus = ThemeMode.values.firstWhere((m) => m.name == j['modus'], orElse: () => ThemeMode.system);
      oled = j['oled'] == true;
    } on FileSystemException {
      // noch nie gespeichert
    } on FormatException {
      // kaputt → Vorgabe
    }
  }

  Future<void> setzen({ThemeMode? modus, bool? oled}) async {
    this.modus = modus ?? this.modus;
    this.oled = oled ?? this.oled;
    notifyListeners();
    await (await _datei()).writeAsString(jsonEncode({'modus': this.modus.name, 'oled': this.oled}));
  }

  String get text => [
        switch (modus) { ThemeMode.light => 'Hell', ThemeMode.dark => 'Dunkel', ThemeMode.system => 'wie System' },
        if (oled && modus != ThemeMode.light) 'OLED-Schwarz',
      ].join(' · ');
}

/// Dunkles Thema; mit oled sind Hintergründe schwarz, Flächen (Karten, Leisten, Menüs) knapp darüber, damit sie
/// sich noch abheben.
ThemeData dunklesThema(Color saat, {required bool oled}) {
  final t = ThemeData(colorSchemeSeed: saat, brightness: Brightness.dark);
  if (!oled) return t;
  final c = t.colorScheme.copyWith(
    surface: Colors.black,
    surfaceDim: Colors.black,
    surfaceContainerLowest: Colors.black,
    surfaceContainerLow: const Color(0xFF0A0A0A),
    surfaceContainer: const Color(0xFF111111),
    surfaceContainerHigh: const Color(0xFF181818),
    surfaceContainerHighest: const Color(0xFF202020),
    surfaceBright: const Color(0xFF262626),
  );
  return ThemeData(colorScheme: c, scaffoldBackgroundColor: Colors.black, canvasColor: Colors.black);
}

/// Ist gerade OLED-Schwarz zu sehen? (für die Karte)
bool oledAktiv(BuildContext context) =>
    Darstellung.instanz.oled && Theme.of(context).brightness == Brightness.dark;

class DarstellungSeite extends StatelessWidget {
  const DarstellungSeite({super.key});

  @override
  Widget build(BuildContext context) {
    final d = Darstellung.instanz;
    return Scaffold(
      appBar: AppBar(title: const Text('Darstellung')),
      body: ListenableBuilder(
        listenable: d,
        builder: (context, _) => ListView(children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: SegmentedButton<ThemeMode>(
              segments: const [
                ButtonSegment(value: ThemeMode.system, label: Text('System'), icon: Icon(Icons.brightness_auto)),
                ButtonSegment(value: ThemeMode.light, label: Text('Hell'), icon: Icon(Icons.light_mode_outlined)),
                ButtonSegment(value: ThemeMode.dark, label: Text('Dunkel'), icon: Icon(Icons.dark_mode_outlined)),
              ],
              selected: {d.modus},
              onSelectionChanged: (s) => d.setzen(modus: s.first),
            ),
          ),
          SwitchListTile(
            secondary: const Icon(Icons.contrast),
            title: const Text('OLED-Schwarz'),
            subtitle: const Text('Im Dunkelmodus echtes Schwarz statt Grau, auch auf der Karte'),
            value: d.oled,
            onChanged: d.modus == ThemeMode.light ? null : (v) => d.setzen(oled: v),
          ),
        ]),
      ),
    );
  }
}
