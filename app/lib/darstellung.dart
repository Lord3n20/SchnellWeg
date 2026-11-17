// Darstellung: hell/dunkel nach System oder fest, im Dunkelmodus wahlweise OLED-Schwarz (echtes Schwarz spart auf
// OLED-Displays Strom), oder „Custom“ mit eigenen Grundfarben (immer dunkel, Vorgabe Schwarz/Cyan).
// Nur auf dem Handy (darstellung.json im App-Ordner); gilt sofort für App und Karte.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

class Darstellung extends ChangeNotifier {
  Darstellung._();
  static final instanz = Darstellung._();

  ThemeMode modus = ThemeMode.system;
  bool oled = false;
  bool custom = false;
  Map<String, Color> farben = Map.of(customVorgabe);

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/darstellung.json');

  /// Vor runApp, damit die App nicht erst hell aufblitzt.
  Future<void> laden() async {
    try {
      final j = jsonDecode(await (await _datei()).readAsString()) as Map<String, dynamic>;
      modus = ThemeMode.values.firstWhere((m) => m.name == j['modus'], orElse: () => ThemeMode.system);
      oled = j['oled'] == true;
      custom = j['custom'] == true;
      final f = j['farben'];
      if (f is Map) {
        for (final k in customVorgabe.keys) {
          if (f[k] is int) farben[k] = Color(f[k] as int);
        }
      }
    } on FileSystemException {
      // noch nie gespeichert
    } on FormatException {
      // kaputt → Vorgabe
    }
  }

  Future<void> setzen({ThemeMode? modus, bool? oled, bool? custom, String? farbe, Color? wert, bool zuruecksetzen = false}) async {
    this.modus = modus ?? this.modus;
    this.oled = oled ?? this.oled;
    this.custom = custom ?? this.custom;
    if (farbe != null && wert != null) farben[farbe] = wert;
    if (zuruecksetzen) farben = Map.of(customVorgabe);
    notifyListeners();
    await (await _datei()).writeAsString(jsonEncode({
      'modus': this.modus.name,
      'oled': this.oled,
      'custom': this.custom,
      'farben': {for (final e in farben.entries) e.key: e.value.toARGB32()},
    }));
  }

  /// Themenmodus für MaterialApp: Custom ist immer dunkel.
  ThemeMode get themenModus => custom ? ThemeMode.dark : modus;

  String get text => custom
      ? 'Custom'
      : [
          switch (modus) { ThemeMode.light => 'Hell', ThemeMode.dark => 'Dunkel', ThemeMode.system => 'wie System' },
          if (oled && modus != ThemeMode.light) 'OLED-Schwarz',
        ].join(' · ');
}

/// Grundfarben für „Custom“; alles Weitere (Linien, gedämpfter Text, Flächenstufen) wird daraus gemischt.
const customVorgabe = <String, Color>{
  'hintergrund': Color(0xFF05090D),
  'flaechen': Color(0xFF0B141A),
  'text': Color(0xFFD6EEEE),
  'akzent': Color(0xFF00FFFF),
  'kontrast': Color(0xFFFF8A1F),
  'kritisch': Color(0xFFFF3B30),
};

const customNamen = <String, String>{
  'hintergrund': 'Hintergrund',
  'flaechen': 'Flächen',
  'text': 'Text',
  'akzent': 'Akzent',
  'kontrast': 'Kontrast',
  'kritisch': 'Kritisch (Fehler, Grenzen)',
};

Color _mix(Color a, Color b, double t) => Color.lerp(a, b, t)!;

Color _auf(Color c) => c.computeLuminance() > 0.4 ? Colors.black : Colors.white;

/// Thema „Custom“: dunkel, dünne Rahmen in der Akzentfarbe, abgerundete Ecken.
ThemeData customThema(Map<String, Color> f) {
  final bg = f['hintergrund']!, fl = f['flaechen']!, tx = f['text']!, ak = f['akzent']!;
  final ko = f['kontrast']!, kr = f['kritisch']!;
  final linie = _mix(bg, ak, 0.22);
  final c = ColorScheme.fromSeed(seedColor: ak, brightness: Brightness.dark).copyWith(
    primary: ak,
    onPrimary: _auf(ak),
    primaryContainer: _mix(bg, ak, 0.37),
    onPrimaryContainer: tx,
    secondary: ak,
    onSecondary: _auf(ak),
    secondaryContainer: _mix(fl, ak, 0.25),
    onSecondaryContainer: tx,
    tertiary: ko,
    onTertiary: _auf(ko),
    tertiaryContainer: _mix(bg, ko, 0.35),
    onTertiaryContainer: tx,
    error: kr,
    onError: _auf(kr),
    errorContainer: _mix(bg, kr, 0.35),
    onErrorContainer: tx,
    surface: bg,
    onSurface: tx,
    onSurfaceVariant: _mix(tx, linie, 0.5),
    surfaceDim: bg,
    surfaceBright: _mix(fl, ak, 0.14),
    surfaceContainerLowest: bg,
    surfaceContainerLow: _mix(bg, fl, 0.6),
    surfaceContainer: fl,
    surfaceContainerHigh: _mix(fl, ak, 0.06),
    surfaceContainerHighest: _mix(fl, ak, 0.1),
    outline: _mix(bg, ak, 0.45),
    outlineVariant: linie,
    inverseSurface: tx,
    onInverseSurface: bg,
    inversePrimary: _mix(bg, ak, 0.6),
  );
  final rund = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(12), side: BorderSide(color: ak.withValues(alpha: 0.28)));
  return ThemeData(
    colorScheme: c,
    scaffoldBackgroundColor: bg,
    canvasColor: bg,
    cardTheme: CardThemeData(color: fl, shape: rund),
    dialogTheme: DialogThemeData(backgroundColor: fl, shape: rund),
    dividerColor: linie,
  );
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
bool oledAktiv(BuildContext context) {
  final d = Darstellung.instanz;
  final schwarz = d.custom ? d.farben['hintergrund']!.computeLuminance() < 0.02 : d.oled;
  return schwarz && Theme.of(context).brightness == Brightness.dark;
}

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
              onSelectionChanged: d.custom ? null : (s) => d.setzen(modus: s.first),
            ),
          ),
          SwitchListTile(
            secondary: const Icon(Icons.contrast),
            title: const Text('OLED-Schwarz'),
            subtitle: const Text('Im Dunkelmodus echtes Schwarz statt Grau, auch auf der Karte'),
            value: d.oled,
            onChanged: d.modus == ThemeMode.light || d.custom ? null : (v) => d.setzen(oled: v),
          ),
          const Divider(),
          SwitchListTile(
            secondary: const Icon(Icons.palette_outlined),
            title: const Text('Custom'),
            subtitle: const Text('Eigene Farben, immer dunkel'),
            value: d.custom,
            onChanged: (v) => d.setzen(custom: v),
          ),
          if (d.custom) ...[
            for (final k in customVorgabe.keys)
              ListTile(
                leading: _Feld(d.farben[k]!),
                title: Text(customNamen[k]!),
                subtitle: Text(_hex(d.farben[k]!)),
                onTap: () async {
                  final c = await showDialog<Color>(
                      context: context, builder: (_) => _FarbDialog(customNamen[k]!, d.farben[k]!));
                  if (c != null) await d.setzen(farbe: k, wert: c);
                },
              ),
            ListTile(
              leading: const Icon(Icons.restart_alt),
              title: const Text('Farben zurücksetzen'),
              onTap: () => d.setzen(zuruecksetzen: true),
            ),
          ],
        ]),
      ),
    );
  }
}

String _hex(Color c) => '#${(c.toARGB32() & 0xFFFFFF).toRadixString(16).padLeft(6, '0').toUpperCase()}';

Color? _ausHex(String s) {
  final t = s.trim().replaceFirst('#', '');
  if (!RegExp(r'^[0-9a-fA-F]{6}$').hasMatch(t)) return null;
  return Color(0xFF000000 | int.parse(t, radix: 16));
}

class _Feld extends StatelessWidget {
  const _Feld(this.farbe, {this.groesse = 32});
  final Color farbe;
  final double groesse;

  @override
  Widget build(BuildContext context) => Container(
        width: groesse,
        height: groesse,
        decoration: BoxDecoration(
          color: farbe,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: Theme.of(context).colorScheme.outline),
        ),
      );
}

/// Farbwahl ohne Zusatzpaket: Vorschläge zum Antippen oder Hex-Wert eintippen.
class _FarbDialog extends StatefulWidget {
  const _FarbDialog(this.titel, this.start);
  final String titel;
  final Color start;

  @override
  State<_FarbDialog> createState() => _FarbDialogState();
}

class _FarbDialogState extends State<_FarbDialog> {
  static const _vorschlaege = [
    0xFF000000, 0xFF05090D, 0xFF0B141A, 0xFF0F1C23, 0xFF1C1C1F, 0xFF2B2B30,
    0xFFFFFFFF, 0xFFD6EEEE, 0xFFE8E8EA, 0xFF9AA0AA, 0xFF6B9494, 0xFF3D6B6B,
    0xFF00FFFF, 0xFF2EC4C4, 0xFF00E676, 0xFF2A78D6, 0xFF7C4DFF, 0xFFFF4081,
    0xFFFF8A1F, 0xFFFFC04D, 0xFFFFEB3B, 0xFFFF3B30, 0xFFC5362A, 0xFF8D6E63,
  ];
  late Color farbe = widget.start;
  late final eingabe = TextEditingController(text: _hex(widget.start));

  @override
  void dispose() {
    eingabe.dispose();
    super.dispose();
  }

  void _setze(Color c) => setState(() {
        farbe = c;
        eingabe.text = _hex(c);
      });

  @override
  Widget build(BuildContext context) => AlertDialog(
        title: Text(widget.titel),
        content: SizedBox(
          width: 320,
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            Wrap(spacing: 8, runSpacing: 8, children: [
              for (final v in _vorschlaege)
                InkWell(
                  borderRadius: BorderRadius.circular(8),
                  onTap: () => _setze(Color(v)),
                  child: _Feld(Color(v), groesse: 36),
                ),
            ]),
            const SizedBox(height: 16),
            Row(children: [
              _Feld(farbe, groesse: 40),
              const SizedBox(width: 12),
              Expanded(
                child: TextField(
                  controller: eingabe,
                  decoration: const InputDecoration(labelText: 'Hex, z. B. #00FFFF'),
                  onChanged: (t) {
                    final c = _ausHex(t);
                    if (c != null) setState(() => farbe = c);
                  },
                ),
              ),
            ]),
          ]),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context), child: const Text('Abbrechen')),
          FilledButton(onPressed: () => Navigator.pop(context, farbe), child: const Text('Übernehmen')),
        ],
      );
}
