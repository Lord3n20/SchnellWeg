// Favoriten: gemerkte Ziele (Name, Art, Koordinaten), nur auf dem Handy (favoriten.json im App-Ordner).
// Stern in der Zielsuche und auf der Startseite; gedrückt halten → umbenennen, in Ordner, löschen. Reihenfolge und
// Ordner auf der Seite „Favoriten ordnen“ (ziehen).
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';

/// Ordner-Überschrift in der Favoritenliste; die Favoriten danach (bis zur nächsten) gehören dazu.
class FavOrdner {
  final String name;
  const FavOrdner(this.name);
}

class Favoriten extends ChangeNotifier {
  Favoriten._();
  static final instanz = Favoriten._();

  /// Reihenfolge wie angezeigt: Ort oder FavOrdner. Vor der ersten Überschrift stehen die Favoriten ohne Ordner.
  List<Object> eintraege = [];
  bool _geladen = false;

  List<Ort> get liste => eintraege.whereType<Ort>().toList();

  /// Favoriten ohne Ordner.
  List<Ort> get oben => eintraege.takeWhile((e) => e is Ort).cast<Ort>().toList();

  /// Ordner mit ihren Favoriten, in Anzeigereihenfolge.
  List<(String, List<Ort>)> get ordner {
    final out = <(String, List<Ort>)>[];
    for (final e in eintraege) {
      if (e is FavOrdner) {
        out.add((e.name, []));
      } else if (out.isNotEmpty) {
        out.last.$2.add(e as Ort);
      }
    }
    return out;
  }

  @visibleForTesting
  void resetFuerTest() => _geladen = false;

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/favoriten.json');

  Future<void> laden() async {
    if (_geladen) return;
    _geladen = true;
    try {
      eintraege = [
        for (final j in (jsonDecode(await (await _datei()).readAsString()) as List).cast<Map<String, dynamic>>())
          if (j['ordner'] case final String n)
            FavOrdner(n)
          else
            Ort(j['art'] as String, j['name'] as String, j['detail'] as String? ?? '', (j['lat'] as num).toDouble(),
                (j['lon'] as num).toDouble())
      ];
    } on FileSystemException {
      // noch keine
    } on FormatException {
      eintraege = [];
    }
    notifyListeners();
  }

  Future<void> _speichern() async {
    notifyListeners();
    await (await _datei()).writeAsString(jsonEncode([
      for (final e in eintraege)
        if (e is Ort)
          {'art': e.art, 'name': e.name, 'detail': e.detail, 'lat': e.lat, 'lon': e.lon}
        else
          {'ordner': (e as FavOrdner).name}
    ]));
  }

  /// Gleicher Ort = höchstens ~20 m auseinander (Name kann umbenannt sein).
  int _index(Ort o) =>
      eintraege.indexWhere((f) => f is Ort && (f.lat - o.lat).abs() < 2e-4 && (f.lon - o.lon).abs() < 3e-4);

  bool hat(Ort o) => _index(o) >= 0;

  /// Neue Favoriten kommen ans Ende der Favoriten ohne Ordner.
  Future<void> umschalten(Ort o) async {
    final i = _index(o);
    if (i >= 0) {
      eintraege.removeAt(i);
    } else {
      eintraege.insert(oben.length, o);
    }
    await _speichern();
  }

  Future<void> umbenennen(Ort o, String name) async {
    final i = _index(o);
    if (i < 0 || name.trim().isEmpty) return;
    eintraege[i] = Ort(o.art, name.trim(), o.detail, o.lat, o.lon);
    await _speichern();
  }

  Future<void> entfernen(Ort o) async {
    final i = _index(o);
    if (i < 0) return;
    eintraege.removeAt(i);
    await _speichern();
  }

  /// Verschiebt o ans Ende des Ordners [name] (null = ohne Ordner).
  Future<void> inOrdner(Ort o, String? name) async {
    final i = _index(o);
    if (i < 0) return;
    final f = eintraege.removeAt(i);
    var k = name == null ? -1 : eintraege.indexWhere((e) => e is FavOrdner && e.name == name);
    k++; // erster Platz nach der Überschrift bzw. ganz oben
    while (k < eintraege.length && eintraege[k] is Ort) {
      k++;
    }
    eintraege.insert(k, f);
    await _speichern();
  }

  Future<void> ordnerNeu(String name) async {
    name = name.trim();
    if (name.isEmpty || ordner.any((o) => o.$1 == name)) return;
    eintraege.add(FavOrdner(name));
    await _speichern();
  }

  Future<void> ordnerUmbenennen(String alt, String neu) async {
    neu = neu.trim();
    final i = eintraege.indexWhere((e) => e is FavOrdner && e.name == alt);
    if (i < 0 || neu.isEmpty || ordner.any((o) => o.$1 == neu)) return;
    eintraege[i] = FavOrdner(neu);
    await _speichern();
  }

  /// Löst den Ordner auf; seine Favoriten kommen zu denen ohne Ordner.
  Future<void> ordnerAufloesen(String name) async {
    final i = eintraege.indexWhere((e) => e is FavOrdner && e.name == name);
    if (i < 0) return;
    final ende = _blockEnde(i);
    final inhalt = eintraege.sublist(i + 1, ende);
    eintraege.removeRange(i, ende);
    eintraege.insertAll(oben.length, inhalt);
    await _speichern();
  }

  int _blockEnde(int i) {
    var e = i + 1;
    while (e < eintraege.length && eintraege[e] is Ort) {
      e++;
    }
    return e;
  }

  /// Ziehen in der Ordnen-Seite (onReorderItem: nach = neuer Platz des Eintrags). Ein Favorit gehört danach zur Überschrift
  /// über ihm; eine Überschrift nimmt ihren ganzen Ordner mit und landet nie mitten in einem anderen.
  Future<void> ziehen(int von, int nach) async {
    if (eintraege[von] is Ort) {
      eintraege.insert(nach, eintraege.removeAt(von));
    } else {
      final ende = _blockEnde(von);
      final block = eintraege.sublist(von, ende);
      eintraege.removeRange(von, ende);
      if (nach > von) nach -= block.length - 1; // Platz in der Liste ohne den Block
      nach = nach.clamp(0, eintraege.length);
      while (nach < eintraege.length && eintraege[nach] is Ort && nach > 0 && _inOrdner(nach)) {
        nach++; // hinter das Ende des Ordners, in den es fallen würde
      }
      if (nach < oben.length) nach = oben.length; // Ordner stehen hinter den Favoriten ohne Ordner
      eintraege.insertAll(nach, block);
    }
    await _speichern();
  }

  bool _inOrdner(int i) => eintraege.sublist(0, i).any((e) => e is FavOrdner);
}

Future<String?> _nameFragen(BuildContext context, String titel, String text) async {
  final feld = TextEditingController(text: text);
  final name = await showDialog<String>(
    context: context,
    builder: (c) => AlertDialog(
      title: Text(titel),
      content: TextField(controller: feld, autofocus: true, onSubmitted: (v) => Navigator.of(c).pop(v)),
      actions: [
        TextButton(onPressed: () => Navigator.of(c).pop(), child: const Text('Abbrechen')),
        FilledButton(onPressed: () => Navigator.of(c).pop(feld.text), child: const Text('Speichern')),
      ],
    ),
  );
  feld.dispose();
  return name;
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

/// Menü beim Gedrückthalten eines Favoriten: umbenennen, in einen Ordner verschieben oder löschen.
Future<void> favoritBearbeiten(BuildContext context, Ort o) async {
  final fav = Favoriten.instanz;
  final wahl = await showModalBottomSheet<String>(
    context: context,
    builder: (c) => SafeArea(
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        ListTile(title: Text(o.name, style: const TextStyle(fontWeight: FontWeight.w600))),
        ListTile(leading: const Icon(Icons.edit), title: const Text('Umbenennen'), onTap: () => Navigator.of(c).pop('name')),
        ListTile(
            leading: const Icon(Icons.drive_file_move_outline),
            title: const Text('In Ordner verschieben'),
            onTap: () => Navigator.of(c).pop('ordner')),
        ListTile(
            leading: const Icon(Icons.swap_vert),
            title: const Text('Favoriten ordnen'),
            onTap: () => Navigator.of(c).pop('ordnen')),
        ListTile(leading: const Icon(Icons.delete_outline), title: const Text('Löschen'), onTap: () => Navigator.of(c).pop('weg')),
      ]),
    ),
  );
  if (!context.mounted) return;
  switch (wahl) {
    case 'weg':
      return fav.entfernen(o);
    case 'ordnen':
      await favoritenOrdnen(context);
    case 'name':
      final name = await _nameFragen(context, 'Favorit umbenennen', o.name);
      if (name != null) await fav.umbenennen(o, name);
    case 'ordner':
      const neu = '\u0000neu', ohne = '\u0000ohne';
      final ziel = await showModalBottomSheet<String>(
        context: context,
        builder: (c) => SafeArea(
          child: ListView(shrinkWrap: true, children: [
            ListTile(
                leading: const Icon(Icons.star_outline), title: const Text('Ohne Ordner'), onTap: () => Navigator.of(c).pop(ohne)),
            for (final (n, _) in fav.ordner)
              ListTile(leading: const Icon(Icons.folder_outlined), title: Text(n), onTap: () => Navigator.of(c).pop(n)),
            ListTile(
                leading: const Icon(Icons.create_new_folder_outlined),
                title: const Text('Neuer Ordner …'),
                onTap: () => Navigator.of(c).pop(neu)),
          ]),
        ),
      );
      if (ziel == null || !context.mounted) return;
      var name = ziel == ohne ? null : ziel;
      if (ziel == neu) {
        name = (await _nameFragen(context, 'Neuer Ordner', ''))?.trim();
        if (name == null || name.isEmpty) return;
        await fav.ordnerNeu(name);
      }
      await fav.inOrdner(o, name);
  }
}

Future<void> favoritenOrdnen(BuildContext context) =>
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const FavoritenSeite()));

/// Ordner-Chip/Eintrag antippen: Auswahl aus dem Ordner; gibt den gewählten Favoriten zurück.
Future<Ort?> ausOrdnerWaehlen(BuildContext context, String name, List<Ort> orte) => showModalBottomSheet<Ort>(
      context: context,
      builder: (c) => SafeArea(
        child: ListView(shrinkWrap: true, children: [
          ListTile(
              leading: const Icon(Icons.folder_open),
              title: Text(name, style: const TextStyle(fontWeight: FontWeight.w600))),
          for (final o in orte)
            ListTile(
              leading: Icon(Icons.star, color: Colors.amber.shade700),
              title: Text(o.name),
              onTap: () => Navigator.of(c).pop(o),
              onLongPress: () => favoritBearbeiten(c, o),
            ),
          if (orte.isEmpty) const ListTile(title: Text('Ordner ist leer')),
        ]),
      ),
    );

/// Favoriten ordnen: ziehen zum Sortieren (auch in/aus Ordnern), Ordner anlegen, umbenennen, auflösen.
class FavoritenSeite extends StatelessWidget {
  const FavoritenSeite({super.key});

  Future<void> _ordnerMenue(BuildContext context, String name) async {
    final fav = Favoriten.instanz;
    final wahl = await showModalBottomSheet<String>(
      context: context,
      builder: (c) => SafeArea(
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          ListTile(title: Text(name, style: const TextStyle(fontWeight: FontWeight.w600))),
          ListTile(leading: const Icon(Icons.edit), title: const Text('Umbenennen'), onTap: () => Navigator.of(c).pop('name')),
          ListTile(
              leading: const Icon(Icons.folder_off_outlined),
              title: const Text('Ordner auflösen (Favoriten bleiben)'),
              onTap: () => Navigator.of(c).pop('weg')),
        ]),
      ),
    );
    if (!context.mounted) return;
    if (wahl == 'weg') return fav.ordnerAufloesen(name);
    if (wahl != 'name') return;
    final neu = await _nameFragen(context, 'Ordner umbenennen', name);
    if (neu != null) await fav.ordnerUmbenennen(name, neu);
  }

  @override
  Widget build(BuildContext context) {
    final fav = Favoriten.instanz;
    return Scaffold(
      appBar: AppBar(title: const Text('Favoriten ordnen')),
      floatingActionButton: FloatingActionButton.extended(
        icon: const Icon(Icons.create_new_folder_outlined),
        label: const Text('Neuer Ordner'),
        onPressed: () async {
          final n = await _nameFragen(context, 'Neuer Ordner', '');
          if (n != null) await fav.ordnerNeu(n);
        },
      ),
      body: ListenableBuilder(
        listenable: fav,
        builder: (context, _) {
          if (fav.eintraege.isEmpty) {
            return const Center(child: Text('Noch keine Favoriten – Stern in der Zielsuche antippen'));
          }
          final ohne = fav.oben.length; // ab hier steht alles in Ordnern → eingerückt
          return ReorderableListView(
            padding: const EdgeInsets.only(bottom: 88),
            buildDefaultDragHandles: false,
            onReorderItem: fav.ziehen,
            children: [
              for (final (i, e) in fav.eintraege.indexed)
                if (e is FavOrdner)
                  ListTile(
                    key: ValueKey('o:${e.name}'),
                    leading: const Icon(Icons.folder_outlined),
                    title: Text(e.name, style: const TextStyle(fontWeight: FontWeight.w600)),
                    trailing: ReorderableDragStartListener(index: i, child: const Icon(Icons.drag_handle)),
                    onTap: () => _ordnerMenue(context, e.name),
                  )
                else
                  ListTile(
                    key: ValueKey('f:${(e as Ort).lat},${e.lon}'),
                    contentPadding: EdgeInsets.only(left: i >= ohne ? 40 : 16, right: 16),
                    leading: Icon(Icons.star, color: Colors.amber.shade700),
                    title: Text(e.name),
                    trailing: ReorderableDragStartListener(index: i, child: const Icon(Icons.drag_handle)),
                    onTap: () => favoritBearbeiten(context, e),
                  ),
            ],
          );
        },
      ),
    );
  }
}
