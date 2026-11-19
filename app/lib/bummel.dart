// Bummelrunde (D-51): ein Modus der Startseite, der von Haus zu Haus führt. Zu Beginn Filter wählen (oder den
// gespeicherten Standard), dann schlägt die App die drei besten noch nicht besuchten Häuser mit Wegzeit vor; eins
// antippen = Route. „Weiter“ hakt das Haus ab (mit Bewertung), „Zurück“ bummelt über andere Häuser zum Ende der
// Runde (Zuhause oder ein vorher gewählter Bund), „Ende“ führt direkt dorthin (D-52). Geplant wird immer als Runde:
// schon auf dem Hinweg zählt, wie weit ein Haus vom Ende wegführt. Die Startseite darf dabei über dem Sperrbildschirm stehen.
// Bewertungen: Reinkommen, Presslust, Fechtsteilheit und politische Ausrichtung (zwei Achsen) liegen auf dem Server
// für alle; „Vorbeigehen“ (0 = Umgangsverbot … 10 = darf nicht fehlen) nur auf dem Handy.
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'halte_db.dart';
import 'korporationen.dart';
import 'standort.dart';
import 'ziel.dart';

const artBummel = 'bummel'; // Ergebnis der Zielsuche: Bummelrunde starten statt Ziel setzen
const vorbeiStandard = 5;

/// Skalen, die der Server sammelt (Schlüssel wie in der API).
enum Skala {
  presslust('presslust', 'Presslust', 'wenig', 'viel'),
  fechten('fechten', 'Fechtsteilheit', 'locker', 'steil'),
  lr('lr', 'Politisch', 'links', 'rechts'),
  lk('lk', 'Gesellschaftlich', 'liberal', 'konservativ');

  final String schluessel, name, links, rechts;
  const Skala(this.schluessel, this.name, this.links, this.rechts);
}

class BummelFilter {
  String gast; // eigener Dachverband, "" = keiner
  int reinMin; // Mindest-Wahrscheinlichkeit reinzukommen in %
  Map<Skala, RangeValues> bereiche;
  int vorbeiMin; // 1–10; 0 (Umgangsverbot) ist immer ausgeschlossen
  bool unbekannt; // Häuser ohne Bewertung trotz eingeschränkter Skala zulassen
  String endeBund; // Ende der Runde: Name eines Hauses, "" = Zuhause

  BummelFilter(
      {this.gast = '', this.reinMin = 0, Map<Skala, RangeValues>? bereiche, this.vorbeiMin = 1, this.unbekannt = true, this.endeBund = ''})
      : bereiche = bereiche ?? {for (final s in Skala.values) s: const RangeValues(1, 10)};

  BummelFilter kopie() => BummelFilter.json(json());

  Map<String, dynamic> json() => {
        'gast': gast,
        'rein_min': reinMin,
        'vorbei_min': vorbeiMin,
        'unbekannt': unbekannt,
        'ende_bund': endeBund,
        for (final e in bereiche.entries) e.key.schluessel: [e.value.start, e.value.end],
      };

  factory BummelFilter.json(Map<String, dynamic> j) {
    RangeValues bereich(Skala s) {
      final l = j[s.schluessel];
      if (l is! List || l.length != 2) return const RangeValues(1, 10);
      return RangeValues((l[0] as num).clamp(1, 10).toDouble(), (l[1] as num).clamp(1, 10).toDouble());
    }

    return BummelFilter(
      gast: j['gast'] as String? ?? '',
      reinMin: (j['rein_min'] as num?)?.toInt() ?? 0,
      vorbeiMin: (j['vorbei_min'] as num?)?.toInt() ?? 1,
      unbekannt: j['unbekannt'] as bool? ?? true,
      endeBund: j['ende_bund'] as String? ?? '',
      bereiche: {for (final s in Skala.values) s: bereich(s)},
    );
  }
}

/// Summe und Anzahl.
typedef Summe = (int, int);

Summe? _summe(Object? l) => l is List && l.length == 2 ? ((l[0] as num).toInt(), (l[1] as num).toInt()) : null;

class HausWerte {
  final Map<String, Summe> rein; // Gast-Dachverband → (reingekommen, Stimmen)
  final Map<Skala, Summe> skalen;
  HausWerte(this.rein, this.skalen);

  HausWerte.json(Map<String, dynamic> j)
      : rein = {
          for (final e in ((j['rein'] as Map?) ?? const {}).entries)
            if (_summe(e.value) case final s?) e.key as String: s
        },
        skalen = {
          for (final s in Skala.values)
            if (_summe(j[s.schluessel]) case final x?) s: x
        };
}

Summe _plus(Summe a, Summe b) => (a.$1 + b.$1, a.$2 + b.$2);

class Bummel extends ChangeNotifier {
  static final instanz = Bummel._();
  Bummel._();

  Api? api; // setzt die Startseite
  String geraet = '';
  BummelFilter standard = BummelFilter();
  Map<String, int> vorbei = {};
  Map<String, Map<String, dynamic>> eigene = {}; // eigene Stimmen je Haus (wie an den Server geschickt)
  BummelFilter? filter; // laufende Runde, null = keine
  List<String> besucht = [];
  bool zurueckPhase = false; // „Zurück“ gedrückt: nur noch Häuser auf dem Weg zum Ende
  Ort? zuhause;
  Map<String, HausWerte> werte = {};
  bool werteGeladen = false;
  Future<void>? _laden;

  bool get aktiv => filter != null;

  /// Ende der laufenden Runde (bzw. des Standards): gewählter Bund oder Zuhause; null = noch nicht festgelegt.
  Ort? endeVon(BummelFilter f) {
    if (f.endeBund.isEmpty) return zuhause;
    return Korporationen.instanz.liste.where((k) => k.name == f.endeBund).firstOrNull?.ort;
  }

  Ort? get endeOrt => filter == null ? null : endeVon(filter!);

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/bummel.json');

  Future<void> laden() => _laden ??= _lesen();

  Future<void> _lesen() async {
    try {
      final j = jsonDecode(await (await _datei()).readAsString()) as Map<String, dynamic>;
      geraet = j['geraet'] as String? ?? '';
      if (j['standard'] case final Map<String, dynamic> s) standard = BummelFilter.json(s);
      vorbei = {for (final e in ((j['vorbei'] as Map?) ?? const {}).entries) e.key as String: (e.value as num).toInt()};
      eigene = {
        for (final e in ((j['eigene'] as Map?) ?? const {}).entries) e.key as String: (e.value as Map).cast<String, dynamic>()
      };
      if (j['zuhause'] case final Map<String, dynamic> z) {
        zuhause = Ort(z['art'] as String, z['name'] as String, z['detail'] as String? ?? '', (z['lat'] as num).toDouble(),
            (z['lon'] as num).toDouble());
      }
      if (j['runde'] case final Map<String, dynamic> r) {
        zurueckPhase = r['zurueck'] == true;
        filter = BummelFilter.json((r['filter'] as Map).cast<String, dynamic>());
        besucht = ((r['besucht'] as List?) ?? const []).cast<String>();
      }
    } catch (_) {
      // erste Nutzung oder kaputte Datei: mit Vorgaben weiter
    }
    if (geraet.isEmpty) {
      final z = Random.secure();
      geraet = List.generate(32, (_) => z.nextInt(16).toRadixString(16)).join();
      await _speichern();
    }
    notifyListeners();
  }

  Future<void> _speichern() async {
    final d = await _datei();
    final tmp = File('${d.path}.neu');
    await tmp.writeAsString(jsonEncode({
      'geraet': geraet,
      'standard': standard.json(),
      'vorbei': vorbei,
      'eigene': eigene,
      if (zuhause case final z?) 'zuhause': {'art': z.art, 'name': z.name, 'detail': z.detail, 'lat': z.lat, 'lon': z.lon},
      if (filter != null) 'runde': {'filter': filter!.json(), 'besucht': besucht, 'zurueck': zurueckPhase},
    }));
    await tmp.rename(d.path);
  }

  /// Summen vom Server holen; ohne Netz bleibt der letzte Stand (oder alles unbekannt).
  Future<void> werteHolen() async {
    final a = api;
    if (a == null) return;
    try {
      final h = await a.bummelWerte();
      werte = {for (final e in h.entries) e.key: HausWerte.json((e.value as Map).cast<String, dynamic>())};
      werteGeladen = true;
      notifyListeners();
    } on ApiFehler {
      // Runde geht auch ohne Server-Werte
    }
  }

  Future<void> starten(BummelFilter f, {bool alsStandard = false}) async {
    filter = f;
    besucht = [];
    zurueckPhase = false;
    if (alsStandard) standard = f.kopie();
    await _speichern();
    notifyListeners();
    werteHolen().ignore();
  }

  Future<void> standardSetzen(BummelFilter f) async {
    standard = f.kopie();
    await _speichern();
    notifyListeners();
  }

  Future<void> abhaken(String haus) async {
    if (!besucht.contains(haus)) besucht.add(haus);
    await _speichern();
    notifyListeners();
  }

  Future<void> zuhauseSetzen(Ort o) async {
    zuhause = Ort(o.art, o.name, o.detail, o.lat, o.lon);
    await _speichern();
    notifyListeners();
  }

  /// „Zurück“: ab jetzt nur noch Häuser, die auf dem Weg zum Ende liegen.
  Future<void> zurueck() async {
    zurueckPhase = true;
    await _speichern();
    notifyListeners();
  }

  Future<void> beenden() async {
    filter = null;
    besucht = [];
    zurueckPhase = false;
    await _speichern();
    notifyListeners();
  }

  int vorbeiVon(Korporation k) => vorbei[k.name] ?? vorbeiStandard;

  Future<void> vorbeiSetzen(Korporation k, int v) async {
    vorbei[k.name] = v.clamp(0, 10).toInt();
    await _speichern();
    notifyListeners();
  }

  /// Eigene Bewertung speichern und an den Server schicken; false = nur auf dem Handy (kein Netz).
  Future<bool> bewerten(Korporation k, {bool? rein, Map<Skala, int?> skalen = const {}, String? gast}) async {
    final stimme = <String, dynamic>{
      'gast': gast ?? filter?.gast ?? standard.gast,
      'rein': ?rein,
      for (final e in skalen.entries)
        if (e.value != null) e.key.schluessel: e.value,
    };
    eigene[k.name] = stimme;
    await _speichern();
    notifyListeners();
    final a = api;
    if (a == null) return false;
    try {
      await a.bummelBewerten({'geraet': geraet, 'haus': k.name, ...stimme});
      await werteHolen();
      return true;
    } on ApiFehler {
      return false;
    }
  }

  // ---- Auswertung ----

  Iterable<Korporation> _gleicherVerband(Korporation k) =>
      Korporationen.instanz.liste.where((x) => k.verband.isNotEmpty && x.verband == k.verband);

  /// Wahrscheinlichkeit reinzukommen für einen Gast aus [gast]: vom Groben zum Feinen, jede Stufe zieht mit
  /// Gewicht 2 zur vorigen – Dachverband des Hauses gesamt → derselbe mit diesem Gast → Haus gesamt → Haus mit Gast.
  /// Ohne jede Stimme 50 %.
  double rein(Korporation k, String gast) {
    Summe gesamt(Map<String, Summe> m) => m.values.fold((0, 0), _plus);
    var dvGesamt = (0, 0), dvGast = (0, 0);
    for (final x in _gleicherVerband(k)) {
      final w = werte[x.name];
      if (w == null) continue;
      dvGesamt = _plus(dvGesamt, gesamt(w.rein));
      dvGast = _plus(dvGast, w.rein[gast] ?? (0, 0));
    }
    final h = werte[k.name];
    double stufe(Summe s, double p0) => (s.$1 + 2 * p0) / (s.$2 + 2);
    var p = 0.5;
    p = stufe(dvGesamt, p);
    p = stufe(dvGast, p);
    p = stufe(h == null ? (0, 0) : gesamt(h.rein), p);
    p = stufe(h?.rein[gast] ?? (0, 0), p);
    return p;
  }

  /// Mittel einer Skala: eigene Stimmen des Hauses, sonst die seines Dachverbands, sonst null.
  double? mittel(Korporation k, Skala s) {
    final h = werte[k.name]?.skalen[s];
    if (h != null && h.$2 > 0) return h.$1 / h.$2;
    var dv = (0, 0);
    for (final x in _gleicherVerband(k)) {
      dv = _plus(dv, werte[x.name]?.skalen[s] ?? (0, 0));
    }
    return dv.$2 > 0 ? dv.$1 / dv.$2 : null;
  }

  /// Stimmen zu diesem Haus insgesamt (für die Anzeige).
  int stimmen(Korporation k) {
    final h = werte[k.name];
    if (h == null) return 0;
    return [h.rein.values.fold(0, (a, s) => a + s.$2), for (final s in h.skalen.values) s.$2].reduce(max);
  }

  /// Anteil der anderen Bünde mit Wert, die auf der Skala unter [k] liegen (0–1), null ohne Wert.
  double? relativ(Korporation k, Skala s) {
    final eigen = mittel(k, s);
    if (eigen == null) return null;
    final andere = [
      for (final x in Korporationen.instanz.liste)
        if (x != k)
          if (werte[x.name]?.skalen[s] case final w? when w.$2 > 0) w.$1 / w.$2
    ];
    if (andere.isEmpty) return null;
    return andere.where((w) => w < eigen).length / andere.length;
  }

  bool passt(Korporation k, BummelFilter f) {
    final v = vorbeiVon(k);
    if (v == 0 || v < f.vorbeiMin) return false;
    if (rein(k, f.gast) * 100 < f.reinMin) return false;
    for (final e in f.bereiche.entries) {
      if (e.value.start <= 1 && e.value.end >= 10) continue; // ganze Skala = kein Filter
      final m = mittel(k, e.key);
      if (m == null ? !f.unbekannt : m < e.value.start || m > e.value.end) return false;
    }
    return true;
  }

  /// Punkte für die Reihenfolge: Vorbeigehen, Reinkommen, Presslust; „darf nicht fehlen“ zieht nach vorn,
  /// jeder Kilometer Luftlinie kostet.
  double punkte(Korporation k, String gast, double meter) {
    final v = vorbeiVon(k);
    return 0.4 * v / 10 + 0.35 * rein(k, gast) + 0.25 * (mittel(k, Skala.presslust) ?? 5) / 10 + (v == 10 ? 0.3 : 0) - 0.12 * meter / 1000;
  }

  /// Die [n] besten noch nicht besuchten Häuser (bis 15 km) für die laufende Runde, als Runde gerechnet:
  /// Hinweg – je km vom Ende weg kostet ein wenig (die Runde soll sich nicht verlaufen);
  /// Zurück – nur Häuser, die näher ans Ende bringen, bewertet nach dem Umweg gegenüber dem direkten Weg.
  List<(Korporation, double)> vorschlaege(double lat, double lon, {int n = 3}) {
    final f = filter;
    if (f == null) return const [];
    final ende = endeOrt;
    final direkt = ende == null ? 0.0 : meterZwischen(lat, lon, ende.lat, ende.lon);
    final l = <(Korporation, double, double)>[];
    for (final k in Korporationen.instanz.liste) {
      if (besucht.contains(k.name) || k.name == f.endeBund || !passt(k, f)) continue;
      final m = meterZwischen(lat, lon, k.lat, k.lon);
      if (m > 15000) continue;
      var p = punkte(k, f.gast, m);
      if (ende != null) {
        final rest = meterZwischen(k.lat, k.lon, ende.lat, ende.lon);
        if (zurueckPhase) {
          final umweg = m + rest - direkt;
          if (rest >= direkt || umweg > max(1000, direkt * 0.5)) continue;
          p -= 0.25 * umweg / 1000;
        } else {
          p -= 0.04 * rest / 1000;
        }
      }
      l.add((k, m, p));
    }
    l.sort((a, b) => b.$3.compareTo(a.$3));
    return [for (final x in l.take(n)) (x.$1, x.$2)];
  }
}

String _prozent(double p) => '${(p * 100).round()} %';

/// Kurze Zeile mit den Werten eines Hauses (Liste, Vorschläge, Blatt).
String bummelKurz(Korporation k, String gast) {
  final b = Bummel.instanz;
  final teile = ['rein ${_prozent(b.rein(k, gast))}'];
  final p = b.mittel(k, Skala.presslust), f = b.mittel(k, Skala.fechten);
  if (p != null) teile.add('Presslust ${p.toStringAsFixed(1).replaceAll('.', ',')}');
  if (f != null) teile.add('Fechten ${f.toStringAsFixed(1).replaceAll('.', ',')}');
  return teile.join(' · ');
}

/// Abschnitt im Blatt eines Hauses: Werte aller, eigenes Vorbeigehen, Bewerten.
class BummelAbschnitt extends StatelessWidget {
  final Korporation k;
  const BummelAbschnitt(this.k, {super.key});

  @override
  Widget build(BuildContext context) => ListenableBuilder(
        listenable: Bummel.instanz,
        builder: (context, _) {
          final b = Bummel.instanz;
          final t = Theme.of(context).textTheme;
          final gast = b.filter?.gast ?? b.standard.gast;
          final v = b.vorbeiVon(k);
          String? politik(Skala s) {
            final m = b.mittel(k, s), r = b.relativ(k, s);
            if (m == null) return null;
            final text = '${s.name}: ${m.toStringAsFixed(1).replaceAll('.', ',')} (1 = ${s.links}, 10 = ${s.rechts})';
            return r == null ? text : '$text · ${s.rechts}er als ${_prozent(r)} der Bünde';
          }

          final pol = [for (final s in [Skala.lr, Skala.lk]) ?politik(s)];
          return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            const Divider(height: 24),
            Text('Bummelrunde', style: t.titleSmall),
            const SizedBox(height: 4),
            Text('${bummelKurz(k, gast)}${gast.isEmpty ? '' : ' (als $gast)'}'
                '${b.werteGeladen ? ' · ${b.stimmen(k)} Stimmen' : ''}'),
            for (final p in pol) Text(p, style: t.bodySmall),
            const SizedBox(height: 8),
            Text('Vorbeigehen: $v – ${vorbeiText(v)}', style: t.bodyMedium),
            Slider(
              value: v.toDouble(),
              min: 0,
              max: 10,
              divisions: 10,
              label: '$v',
              onChanged: (x) => b.vorbeiSetzen(k, x.round()),
            ),
            Align(
              alignment: Alignment.centerRight,
              child: OutlinedButton.icon(
                onPressed: () => bummelBewerten(context, k),
                icon: const Icon(Icons.rate_review_outlined, size: 18),
                label: Text(b.eigene.containsKey(k.name) ? 'Bewertung ändern' : 'Bewerten'),
              ),
            ),
          ]);
        },
      );
}

String vorbeiText(int v) => switch (v) {
      0 => 'Umgangsverbot, nie vorschlagen',
      10 => 'darf nicht fehlen',
      <= 3 => 'eher meiden',
      >= 7 => 'gern',
      _ => 'neutral',
    };

/// Bewertung eines Hauses (alles freiwillig); Vorbeigehen bleibt auf dem Handy.
Future<void> bummelBewerten(BuildContext context, Korporation k) async {
  final b = Bummel.instanz;
  final alt = b.eigene[k.name] ?? const {};
  bool? rein = alt['rein'] as bool?;
  final skalen = <Skala, int?>{for (final s in Skala.values) s: (alt[s.schluessel] as num?)?.toInt()};
  var vorbei = b.vorbeiVon(k);
  final m = ScaffoldMessenger.of(context);
  final ok = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, setze) {
        final t = Theme.of(ctx).textTheme;
        Widget skala(Skala s) {
          final w = skalen[s];
          return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Expanded(child: Text(w == null ? '${s.name}: keine Angabe' : '${s.name}: $w', style: t.bodyMedium)),
              if (w != null) TextButton(onPressed: () => setze(() => skalen[s] = null), child: const Text('keine Angabe')),
            ]),
            Row(children: [
              SizedBox(width: 72, child: Text(s.links, style: t.bodySmall)),
              Expanded(
                child: Slider(
                  value: (w ?? 5.5).toDouble(),
                  min: 1,
                  max: 10,
                  divisions: w == null ? null : 9,
                  label: w == null ? null : '$w',
                  onChanged: (x) => setze(() => skalen[s] = x.round()),
                ),
              ),
              SizedBox(width: 72, child: Text(s.rechts, textAlign: TextAlign.end, style: t.bodySmall)),
            ]),
          ]);
        }

        return SafeArea(
          child: SingleChildScrollView(
            padding: EdgeInsets.fromLTRB(20, 0, 20, 16 + MediaQuery.of(ctx).viewInsets.bottom),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(k.name, style: t.titleMedium),
              const SizedBox(height: 12),
              Text('Reingekommen?', style: t.bodyMedium),
              const SizedBox(height: 4),
              SegmentedButton<int>(
                segments: const [
                  ButtonSegment(value: 1, label: Text('Ja')),
                  ButtonSegment(value: 0, label: Text('Nein')),
                  ButtonSegment(value: -1, label: Text('keine Angabe')),
                ],
                selected: {rein == null ? -1 : (rein! ? 1 : 0)},
                onSelectionChanged: (s) => setze(() => rein = switch (s.first) { 1 => true, 0 => false, _ => null }),
              ),
              const SizedBox(height: 12),
              for (final s in Skala.values) skala(s),
              const Divider(height: 24),
              Text('Vorbeigehen (nur für dich): $vorbei – ${vorbeiText(vorbei)}', style: t.bodyMedium),
              Slider(
                value: vorbei.toDouble(),
                min: 0,
                max: 10,
                divisions: 10,
                label: '$vorbei',
                onChanged: (x) => setze(() => vorbei = x.round()),
              ),
              const SizedBox(height: 8),
              Row(children: [
                TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Überspringen')),
                const Spacer(),
                FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Speichern')),
              ]),
            ]),
          ),
        );
      },
    ),
  );
  if (ok != true) return;
  await b.vorbeiSetzen(k, vorbei);
  final geschickt = await b.bewerten(k, rein: rein, skalen: skalen);
  if (!geschickt) m.showSnackBar(const SnackBar(content: Text('Bewertung nur auf dem Handy gespeichert (Server nicht erreichbar).')));
}

/// Beginn einer Runde: Filter wählen, als Standard speichern; true = Runde gestartet.
class BummelStart extends StatefulWidget {
  final Api api;
  final HalteDb db;
  final Position? nahe;
  const BummelStart({super.key, required this.api, required this.db, this.nahe});

  @override
  State<BummelStart> createState() => _BummelStartState();
}

class _BummelStartState extends State<BummelStart> {
  BummelFilter _f = BummelFilter();
  bool _bereit = false;

  Future<void> _zuhauseWaehlen() async {
    final o = await Navigator.of(context).push<Ort>(MaterialPageRoute(
        builder: (_) => ZielSuche(
            api: widget.api, db: widget.db, nahe: widget.nahe, hinweis: 'Zuhause: Adresse, Haltestelle …', mitBummel: false)));
    if (o == null) return;
    await Bummel.instanz.zuhauseSetzen(o);
    if (mounted) setState(() => _f.endeBund = '');
  }

  Widget _endeWahl(TextTheme t) {
    final b = Bummel.instanz;
    final haeuser = [for (final k in Korporationen.instanz.liste) k.name]..sort();
    final bund = _f.endeBund.isNotEmpty;
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text('Ende der Runde („Ende“ führt direkt hin, „Zurück“ bummelt dorthin)', style: t.bodyMedium),
      const SizedBox(height: 6),
      SegmentedButton<bool>(
        segments: const [
          ButtonSegment(value: false, label: Text('Zuhause'), icon: Icon(Icons.home_outlined)),
          ButtonSegment(value: true, label: Text('Bund'), icon: Icon(Icons.flag_outlined)),
        ],
        selected: {bund},
        onSelectionChanged: (s) => setState(() => _f.endeBund = s.first ? (haeuser.isEmpty ? '' : haeuser.first) : ''),
      ),
      if (!bund)
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.home_outlined),
          title: Text(b.zuhause?.name ?? 'Zuhause festlegen'),
          subtitle: b.zuhause == null ? const Text('nötig für die Runde') : const Text('antippen zum Ändern'),
          onTap: _zuhauseWaehlen,
        )
      else
        Padding(
          padding: const EdgeInsets.only(top: 10),
          child: DropdownButtonFormField<String>(
            key: ValueKey('ende${_f.endeBund}'),
            initialValue: haeuser.contains(_f.endeBund) ? _f.endeBund : null,
            isExpanded: true,
            decoration: const InputDecoration(labelText: 'Bund am Ende', border: OutlineInputBorder(), isDense: true),
            items: [for (final h in haeuser) DropdownMenuItem(value: h, child: Text(h, overflow: TextOverflow.ellipsis))],
            onChanged: (v) => setState(() => _f.endeBund = v ?? ''),
          ),
        ),
      const SizedBox(height: 8),
    ]);
  }

  @override
  void initState() {
    super.initState();
    Future.wait([Bummel.instanz.laden(), Korporationen.instanz.laden()]).then((_) {
      if (!mounted) return;
      setState(() {
        _f = Bummel.instanz.standard.kopie();
        _bereit = true;
      });
      Bummel.instanz.werteHolen();
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context).textTheme;
    final verbaende = {for (final k in Korporationen.instanz.liste) if (k.verband.isNotEmpty) k.verband}.toList()..sort();
    if (_f.gast.isNotEmpty && !verbaende.contains(_f.gast)) verbaende.insert(0, _f.gast);
    String zahl(double x) => x.round().toString();
    return Scaffold(
      appBar: AppBar(title: const Text('Bummelrunde')),
      body: !_bereit
          ? const Center(child: CircularProgressIndicator())
          : ListView(padding: const EdgeInsets.fromLTRB(16, 8, 16, 24), children: [
              DropdownButtonFormField<String>(
                initialValue: _f.gast,
                decoration: const InputDecoration(labelText: 'Mein Dachverband', border: OutlineInputBorder(), isDense: true),
                items: [
                  const DropdownMenuItem(value: '', child: Text('keiner / egal')),
                  for (final v in verbaende) DropdownMenuItem(value: v, child: Text(v)),
                ],
                onChanged: (v) => setState(() => _f.gast = v ?? ''),
              ),
              const SizedBox(height: 16),
              _endeWahl(t),
              Text('Reinkommen mindestens ${_f.reinMin} %', style: t.bodyMedium),
              Slider(
                value: _f.reinMin.toDouble(),
                min: 0,
                max: 90,
                divisions: 9,
                label: '${_f.reinMin} %',
                onChanged: (x) => setState(() => _f.reinMin = x.round()),
              ),
              for (final s in Skala.values) ...[
                Text(
                    '${s.name}: ${zahl(_f.bereiche[s]!.start)}–${zahl(_f.bereiche[s]!.end)}'
                    '${_f.bereiche[s]!.start <= 1 && _f.bereiche[s]!.end >= 10 ? ' (alle)' : ''}',
                    style: t.bodyMedium),
                Row(children: [
                  SizedBox(width: 72, child: Text(s.links, style: t.bodySmall)),
                  Expanded(
                    child: RangeSlider(
                      values: _f.bereiche[s]!,
                      min: 1,
                      max: 10,
                      divisions: 9,
                      labels: RangeLabels(zahl(_f.bereiche[s]!.start), zahl(_f.bereiche[s]!.end)),
                      onChanged: (r) => setState(() => _f.bereiche[s] = r),
                    ),
                  ),
                  SizedBox(width: 72, child: Text(s.rechts, textAlign: TextAlign.end, style: t.bodySmall)),
                ]),
              ],
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Häuser ohne Bewertung zulassen'),
                subtitle: const Text('gilt für eingeschränkte Skalen'),
                value: _f.unbekannt,
                onChanged: (x) => setState(() => _f.unbekannt = x),
              ),
              Text('Vorbeigehen mindestens ${_f.vorbeiMin} (0 = Umgangsverbot ist immer raus)', style: t.bodyMedium),
              Slider(
                value: _f.vorbeiMin.toDouble(),
                min: 1,
                max: 10,
                divisions: 9,
                label: '${_f.vorbeiMin}',
                onChanged: (x) => setState(() => _f.vorbeiMin = x.round()),
              ),
              const SizedBox(height: 8),
              Row(children: [
                TextButton(
                  onPressed: () async {
                    await Bummel.instanz.standardSetzen(_f);
                    if (context.mounted) {
                      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Als Standard gespeichert.')));
                    }
                  },
                  child: const Text('Als Standard speichern'),
                ),
                TextButton(
                  onPressed: () => setState(() => _f = BummelFilter(gast: _f.gast, endeBund: _f.endeBund)),
                  child: const Text('Zurücksetzen'),
                ),
              ]),
              const SizedBox(height: 8),
              FilledButton.icon(
                onPressed: Bummel.instanz.endeVon(_f) == null
                    ? null
                    : () async {
                  await Bummel.instanz.starten(_f.kopie());
                  if (context.mounted) Navigator.pop(context, true);
                },
                icon: const Icon(Icons.sports_bar_outlined),
                label: const Text('Los'),
              ),
            ]),
    );
  }
}

/// Leiste über der Liste, solange eine Runde läuft.
class BummelLeiste extends StatelessWidget {
  final VoidCallback? onWeiter, onZurueck;
  final VoidCallback onEnde;
  const BummelLeiste({super.key, required this.onWeiter, required this.onZurueck, required this.onEnde});

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final b = Bummel.instanz;
    final n = b.besucht.length;
    const eng = ButtonStyle(
      visualDensity: VisualDensity.compact,
      padding: WidgetStatePropertyAll(EdgeInsets.symmetric(horizontal: 12)),
    );
    return Container(
      color: farbe.secondaryContainer,
      padding: const EdgeInsets.fromLTRB(12, 2, 8, 2),
      child: Row(children: [
        Icon(b.zurueckPhase ? Icons.u_turn_left : Icons.sports_bar_outlined, color: farbe.onSecondaryContainer),
        const SizedBox(width: 6),
        Expanded(
          child: Text(b.zurueckPhase ? 'zurück · $n' : '$n besucht',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontWeight: FontWeight.w600, color: farbe.onSecondaryContainer)),
        ),
        FilledButton(style: eng, onPressed: onWeiter, child: const Text('Weiter')),
        const SizedBox(width: 4),
        OutlinedButton(style: eng, onPressed: onZurueck, child: const Text('Zurück')),
        const SizedBox(width: 4),
        OutlinedButton(style: eng, onPressed: onEnde, child: const Text('Ende')),
      ]),
    );
  }
}
