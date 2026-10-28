// Favoriten: gemerkte Ziele (Name, Art, Koordinaten), nur auf dem Handy (favoriten.json im App-Ordner).
// Stern in der Zielsuche und auf der Startseite; gedrückt halten → umbenennen oder löschen.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';

class Favoriten extends ChangeNotifier {
  Favoriten._();
  static final instanz = Favoriten._();

  List<Ort> liste = [];
  bool _geladen = false;

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/favoriten.json');

  Future<void> laden() async {
    if (_geladen) return;
    _geladen = true;
    try {
      liste = [
        for (final j in (jsonDecode(await (await _datei()).readAsString()) as List).cast<Map<String, dynamic>>())
          Ort(j['art'] as String, j['name'] as String, j['detail'] as String? ?? '', (j['lat'] as num).toDouble(),
              (j['lon'] as num).toDouble())
      ];
    } on FileSystemException {
      // noch keine
    } on FormatException {
      liste = [];
    }
    notifyListeners();
  }

  Future<void> _speichern() async {
    notifyListeners();
    await (await _datei()).writeAsString(jsonEncode([
      for (final o in liste) {'art': o.art, 'name': o.name, 'detail': o.detail, 'lat': o.lat, 'lon': o.lon}
    ]));
  }

  /// Gleicher Ort = höchstens ~20 m auseinander (Name kann umbenannt sein).
  int _index(Ort o) => liste.indexWhere((f) => (f.lat - o.lat).abs() < 2e-4 && (f.lon - o.lon).abs() < 3e-4);

  bool hat(Ort o) => _index(o) >= 0;

  Future<void> umschalten(Ort o) async {
    final i = _index(o);
    if (i >= 0) {
      liste.removeAt(i);
    } else {
      liste.add(o);
    }
    await _speichern();
  }

  Future<void> umbenennen(Ort o, String name) async {
    final i = _index(o);
    if (i < 0 || name.trim().isEmpty) return;
    liste[i] = Ort(o.art, name.trim(), o.detail, o.lat, o.lon);
    await _speichern();
  }

  Future<void> entfernen(Ort o) async {
    final i = _index(o);
    if (i < 0) return;
    liste.removeAt(i);
    await _speichern();
  }
}

/// Stern zum Merken/Vergessen eines Ziels.
class FavoritStern extends StatelessWidget {
  final Ort ort;
  final bool klein; // in einer Textzeile: ohne Mindestgröße
  const FavoritStern(this.ort, {super.key, this.klein = false});

  @override
  Widget build(BuildContext context) => ListenableBuilder(
        listenable: Favoriten.instanz,
        builder: (context, _) {
          final da = Favoriten.instanz.hat(ort);
          return IconButton(
            icon: Icon(da ? Icons.star : Icons.star_border, color: da ? Colors.amber.shade700 : null),
            tooltip: da ? 'Aus Favoriten entfernen' : 'Als Favorit merken',
            onPressed: () => Favoriten.instanz.umschalten(ort),
            padding: klein ? EdgeInsets.zero : null,
            constraints: klein ? const BoxConstraints() : null,
            iconSize: klein ? 22 : null,
          );
        },
      );
}

/// Menü beim Gedrückthalten eines Favoriten: umbenennen oder löschen.
Future<void> favoritBearbeiten(BuildContext context, Ort o) async {
  final wahl = await showModalBottomSheet<String>(
    context: context,
    builder: (c) => SafeArea(
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        ListTile(title: Text(o.name, style: const TextStyle(fontWeight: FontWeight.w600))),
        ListTile(leading: const Icon(Icons.edit), title: const Text('Umbenennen'), onTap: () => Navigator.of(c).pop('name')),
        ListTile(leading: const Icon(Icons.delete_outline), title: const Text('Löschen'), onTap: () => Navigator.of(c).pop('weg')),
      ]),
    ),
  );
  if (!context.mounted) return;
  if (wahl == 'weg') return Favoriten.instanz.entfernen(o);
  if (wahl != 'name') return;
  final feld = TextEditingController(text: o.name);
  final name = await showDialog<String>(
    context: context,
    builder: (c) => AlertDialog(
      title: const Text('Favorit umbenennen'),
      content: TextField(controller: feld, autofocus: true, onSubmitted: (v) => Navigator.of(c).pop(v)),
      actions: [
        TextButton(onPressed: () => Navigator.of(c).pop(), child: const Text('Abbrechen')),
        FilledButton(onPressed: () => Navigator.of(c).pop(feld.text), child: const Text('Speichern')),
      ],
    ),
  );
  feld.dispose();
  if (name != null) await Favoriten.instanz.umbenennen(o, name);
}
