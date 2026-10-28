// Einstellungen als Menü mit Unterpunkten: Tempo-Profile, Offline-Karten (laden/löschen, belegter Platz), Darstellung,
// Favoriten, Zugang.
import 'package:flutter/material.dart';

import 'api.dart';
import 'darstellung.dart';
import 'einstellungen.dart';
import 'favoriten.dart';
import 'karte.dart' show groesse;
import 'kartenlager.dart';
import 'tempo.dart';
import 'tempo_messen.dart';
import 'zugang.dart';

class EinstellungenSeite extends StatefulWidget {
  final TempoProfil tempo;
  final Zugang? zugang;
  final ValueChanged<TempoProfil> tempoGeaendert;
  final ValueChanged<Zugang> zugangGeaendert;

  const EinstellungenSeite({
    super.key,
    required this.tempo,
    required this.zugang,
    required this.tempoGeaendert,
    required this.zugangGeaendert,
  });

  @override
  State<EinstellungenSeite> createState() => _EinstellungenSeiteState();
}

class _EinstellungenSeiteState extends State<EinstellungenSeite> with _LagerBeobachten {
  late TempoProfil _tempo = widget.tempo;
  late Zugang? _zugang = widget.zugang;

  Future<void> _tempoAendern() async {
    await Navigator.of(context).push(MaterialPageRoute(builder: (_) => const TempoSeite()));
    final p = await Tempo.laden();
    if (!mounted || (p.name == _tempo.name && p.kmh == _tempo.kmh)) return;
    setState(() => _tempo = p);
    widget.tempoGeaendert(p);
  }

  Future<void> _zugangAendern() async {
    final z = await Navigator.of(context).push<Zugang>(MaterialPageRoute(builder: (_) => Einstellungen(alt: _zugang)));
    if (z == null || !mounted) return;
    setState(() => _zugang = z);
    widget.zugangGeaendert(z);
  }

  @override
  Widget build(BuildContext context) {
    final n = lager.regionen.where((r) => r.id != uebersicht && lager.hat(r.id)).length;
    final laeuft = lager.laedt.keys.any((id) => id != uebersicht);
    return Scaffold(
      appBar: AppBar(title: const Text('Einstellungen')),
      body: ListView(children: [
        _Punkt(
          icon: switch (_tempo.art) {
            'fuss' => Icons.directions_walk,
            'einrad' => Icons.circle_outlined,
            _ => Icons.directions_bike,
          },
          titel: 'Tempo',
          unter: '${_tempo.name} · ${tempoText(_tempo.kmh)}',
          onTap: _tempoAendern,
        ),
        _Punkt(
          icon: Icons.map_outlined,
          titel: 'Offline-Karten',
          unter: [
            n == 0 ? 'nur Übersicht' : (n == 1 ? '1 Bundesland' : '$n Bundesländer'),
            if (belegt != null) '${groesse(belegt!)} belegt',
            if (laeuft) 'lädt …',
          ].join(' · '),
          onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const KartenSeite())),
        ),
        ListenableBuilder(
          listenable: Darstellung.instanz,
          builder: (context, _) => _Punkt(
            icon: Icons.contrast,
            titel: 'Darstellung',
            unter: Darstellung.instanz.text,
            onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const DarstellungSeite())),
          ),
        ),
        _Punkt(
          icon: Icons.star_outline,
          titel: 'Favoriten',
          unter: 'sortieren, Ordner anlegen',
          onTap: () => favoritenOrdnen(context),
        ),
        _Punkt(
          icon: Icons.key,
          titel: 'Zugang',
          unter: _zugang?.basis.host ?? 'nicht eingerichtet',
          onTap: _zugangAendern,
        ),
      ]),
    );
  }
}

/// Unterpunkt „Offline-Karten“: geladene Bundesländer löschen, weitere laden.
class KartenSeite extends StatefulWidget {
  const KartenSeite({super.key});

  @override
  State<KartenSeite> createState() => _KartenSeiteState();
}

class _KartenSeiteState extends State<KartenSeite> with _LagerBeobachten {
  Future<void> _loeschen(KartenRegion r) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text('Karte ${r.name} löschen?'),
        content: Text('Gibt etwa ${groesse(r.bytes)} frei. Dort zeigt die Karte danach nur die Übersicht; '
            'du kannst sie jederzeit neu laden.'),
        actions: [
          TextButton(onPressed: () => Navigator.of(c).pop(false), child: const Text('Abbrechen')),
          FilledButton(onPressed: () => Navigator.of(c).pop(true), child: const Text('Löschen')),
        ],
      ),
    );
    if (ok != true) return;
    await lager.loeschen(r.id);
    belegtNeu(); // die Datei ist erst nach loeschen() weg
  }

  @override
  Widget build(BuildContext context) {
    final regionen = lager.regionen.where((r) => r.id != uebersicht).toList()..sort((a, b) => a.name.compareTo(b.name));
    final aufHandy = [for (final r in regionen) if (lager.hat(r.id) || lager.laedt.containsKey(r.id)) r];
    final weitere = [for (final r in regionen) if (!aufHandy.contains(r)) r];
    final ueb = lager.regionen.where((r) => r.id == uebersicht).firstOrNull;

    return Scaffold(
      appBar: AppBar(title: const Text('Offline-Karten')),
      body: ListView(children: [
        if (lager.fehler != null)
          ListTile(
            leading: Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
            title: Text(lager.fehler!),
          ),
        _Ueberschrift('Auf dem Handy', rechts: belegt == null ? null : '${groesse(belegt!)} belegt'),
        ListTile(
          leading: const Icon(Icons.public),
          title: const Text('Übersicht Deutschland'),
          subtitle: Text(lager.laedt.containsKey(uebersicht)
              ? 'wird geladen …'
              : 'immer dabei${ueb == null ? '' : ' · ${groesse(ueb.bytes)}'}'),
        ),
        for (final r in aufHandy) _zeile(r),
        if (weitere.isNotEmpty) ...[
          const Divider(),
          const _Ueberschrift('Weitere Bundesländer'),
          for (final r in weitere) _zeile(r),
        ] else if (regionen.isEmpty)
          const ListTile(title: Text('Kartenliste noch nicht geladen')),
      ]),
    );
  }

  Widget _zeile(KartenRegion r) {
    final p = lager.laedt[r.id];
    final da = lager.hat(r.id);
    return ListTile(
      leading: Icon(da ? Icons.map : Icons.map_outlined),
      title: Text(r.name),
      subtitle: p != null && !da
          ? Padding(
              padding: const EdgeInsets.only(top: 6),
              child: LinearProgressIndicator(value: p >= 0 ? p : null),
            )
          : Text('${groesse(r.bytes)}${da ? ' · Stand ${_datum(r.stand)}' : ''}'),
      trailing: p != null
          ? const SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2))
          : da
              ? IconButton(icon: const Icon(Icons.delete_outline), tooltip: 'Löschen', onPressed: () => _loeschen(r))
              : IconButton(icon: const Icon(Icons.download), tooltip: 'Laden', onPressed: () => lager.laden(r.id)),
    );
  }
}

/// Baut bei jeder Änderung im KartenLager neu und hält den belegten Platz aktuell.
mixin _LagerBeobachten<T extends StatefulWidget> on State<T> {
  final lager = KartenLager.instanz;
  int? belegt;
  int _belegtStand = -1;

  @override
  void initState() {
    super.initState();
    lager.addListener(_geaendert);
    belegtNeu();
  }

  @override
  void dispose() {
    lager.removeListener(_geaendert);
    super.dispose();
  }

  void _geaendert() {
    if (!mounted) return;
    setState(() {});
    if (_belegtStand != lager.stand) belegtNeu();
  }

  void belegtNeu() {
    _belegtStand = lager.stand;
    lager.belegt().then((n) {
      if (mounted) setState(() => belegt = n);
    });
  }
}

class _Punkt extends StatelessWidget {
  final IconData icon;
  final String titel, unter;
  final VoidCallback onTap;
  const _Punkt({required this.icon, required this.titel, required this.unter, required this.onTap});

  @override
  Widget build(BuildContext context) => ListTile(
        leading: Icon(icon),
        title: Text(titel),
        subtitle: Text(unter),
        trailing: const Icon(Icons.chevron_right),
        onTap: onTap,
      );
}

class _Ueberschrift extends StatelessWidget {
  final String text;
  final String? rechts;
  const _Ueberschrift(this.text, {this.rechts});

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
      child: Row(children: [
        Expanded(child: Text(text, style: t.textTheme.labelLarge?.copyWith(color: t.colorScheme.primary))),
        if (rechts != null) Text(rechts!, style: t.textTheme.bodySmall),
      ]),
    );
  }
}

String _datum(DateTime d) {
  final l = d.toLocal();
  return '${l.day}.${l.month}.${l.year}';
}
