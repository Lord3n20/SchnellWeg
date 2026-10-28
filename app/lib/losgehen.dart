// Losgeh-Wecker (D-29): nächster Termin mit Ort aus dem Android-Kalender → Ort suchen → Verbindung mit Ankunft bis
// Terminbeginn − Puffer → Meldung zur Losgehzeit − Vorlauf. Gerechnet wird nur, wenn die App offen ist (Start und
// Rückkehr, höchstens alle 5 min); der gestellte Wecker klingelt auch bei geschlossener App. 10 min vor der Meldung
// rechnet die App einmal im Hintergrund mit Echtzeit nach (gleicher Start wie bei der letzten Rechnung, siehe
// [losgehNachrechnen]). Termine bleiben auf dem Handy, der Server sieht nur den Ortstext (wie eine Suche) und die
// Route. Einstellungen und der letzte Auftrag in losgehen.json.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'standort.dart';
import 'tempo.dart';
import 'zugang.dart';

const _kanal = MethodChannel('schnellweg/losgehen');
const _vorschau = 12; // Stunden: so weit voraus zählt ein Termin (und nur heute)
const _nachrechnenVorher = Duration(minutes: 10); // so lange vor der Meldung im Hintergrund nachrechnen

/// Einstieg für den Hintergrund (LosgehWecker.Nachrechner startet dafür eine Flutter-Engine ohne Oberfläche).
@pragma('vm:entry-point')
Future<void> losgehNachrechnen() async {
  WidgetsFlutterBinding.ensureInitialized();
  try {
    await Losgehen.instanz.nachrechnen();
  } catch (_) {
    // alter Wecker bleibt stehen
  } finally {
    await _kanal.invokeMethod('fertig');
  }
}

class Termin {
  final String titel, ort;
  final DateTime beginn;
  Termin(this.titel, this.ort, this.beginn);
}

/// Ergebnis für den nächsten Termin: [ziel]/[v] null, wenn der Ort nicht gefunden wurde bzw. keine Verbindung.
class Losgehplan {
  final Termin termin;
  final Ort? ziel;
  final Verbindung? v;
  Losgehplan(this.termin, this.ziel, this.v);
}

class Losgehen extends ChangeNotifier {
  Losgehen._();
  static final instanz = Losgehen._();

  bool an = false;
  int vorlauf = 5; // Minuten vor der Losgehzeit melden
  int puffer = 5; // Minuten vor Terminbeginn ankommen
  Losgehplan? plan;
  Map<String, dynamic>? _auftrag; // letzte Rechnung: Start, Ziel, Ankunft, Tempo, Termin (für das Nachrechnen)
  DateTime? _zuletzt;
  final _orte = <String, Ort?>{}; // Ortstext → Suchtreffer, solange die App läuft

  static Future<File> _datei() async => File('${(await getApplicationSupportDirectory()).path}/losgehen.json');

  Future<void> laden() async {
    try {
      final j = jsonDecode(await (await _datei()).readAsString()) as Map<String, dynamic>;
      an = j['an'] == true;
      vorlauf = (j['vorlauf'] as int?) ?? vorlauf;
      puffer = (j['puffer'] as int?) ?? puffer;
      _auftrag = j['auftrag'] as Map<String, dynamic>?;
    } on FileSystemException {
      // noch nie gespeichert
    } on FormatException {
      // kaputt → Vorgabe
    }
    notifyListeners();
  }

  /// Einschalten fragt nach der Kalender-Erlaubnis; ohne bleibt er aus. Ergebnis: ob er jetzt an ist.
  Future<bool> setzen({bool? an, int? vorlauf, int? puffer}) async {
    if (an == true && !this.an && await _kanal.invokeMethod<bool>('erbitten') != true) return false;
    this.an = an ?? this.an;
    this.vorlauf = vorlauf ?? this.vorlauf;
    this.puffer = puffer ?? this.puffer;
    _zuletzt = null; // neu rechnen
    if (!this.an) {
      plan = null;
      _auftrag = null;
      await _kanal.invokeMethod('loeschen');
    }
    notifyListeners();
    await _speichern();
    return this.an;
  }

  Future<void> _speichern() async => (await _datei()).writeAsString(
      jsonEncode({'an': an, 'vorlauf': vorlauf, 'puffer': puffer, 'auftrag': ?_auftrag}));

  /// Nächsten Termin neu planen und den Wecker stellen; [immer] auch, wenn die letzte Rechnung frisch ist.
  Future<void> pruefen(Api api, Position pos, TempoProfil tempo, {bool immer = false}) async {
    if (!an) return;
    final jetzt = DateTime.now();
    if (!immer && _zuletzt != null && jetzt.difference(_zuletzt!) < const Duration(minutes: 5)) return;
    _zuletzt = jetzt;
    final roh = await _kanal.invokeListMethod<Map>('termine', {'stunden': _vorschau});
    final t = [
      for (final m in roh ?? const <Map>[])
        Termin(m['titel'] as String, m['ort'] as String, DateTime.fromMillisecondsSinceEpoch(m['beginn'] as int))
    ].where((t) => t.beginn.isAfter(jetzt.add(Duration(minutes: puffer))) && t.beginn.day == jetzt.day).firstOrNull;
    // nur heute: der Server hat nur den heutigen Betriebstag geladen (D-27)
    if (t == null) {
      plan = null;
      await _kanal.invokeMethod('loeschen');
      return notifyListeners();
    }
    try {
      if (!_orte.containsKey(t.ort)) _orte[t.ort] = (await api.suche(t.ort, n: 1, nahe: pos)).firstOrNull;
      final ziel = _orte[t.ort];
      Verbindung? v;
      _auftrag = null;
      if (ziel != null) {
        _auftrag = {
          'von': [pos.lat, pos.lon],
          'ziel': {'kind': ziel.art, 'name': ziel.name, 'detail': ziel.detail, 'lat': ziel.lat, 'lon': ziel.lon},
          'bis': t.beginn.subtract(Duration(minutes: puffer)).millisecondsSinceEpoch,
          'kmh': tempo.kmh,
          'art': tempo.art,
          'titel': t.titel,
          'ort': t.ort,
          'beginn': t.beginn.millisecondsSinceEpoch,
        };
        v = await _spaeteste(api, _auftrag!);
      }
      plan = Losgehplan(t, ziel, v);
    } on ApiFehler {
      plan = Losgehplan(t, null, null);
    }
    await _speichern();
    await _weckerStellen();
    notifyListeners();
  }

  /// Im Hintergrund kurz vor der Meldung: letzten Auftrag mit aktueller Echtzeit neu rechnen und den Wecker
  /// umstellen. Ohne Ergebnis (Netz, keine Verbindung) bleibt der alte Wecker.
  Future<void> nachrechnen() async {
    await laden();
    final a = _auftrag, zugang = await Zugang.laden();
    if (!an || a == null || zugang == null) return;
    final v = await _spaeteste(Api(zugang), a);
    if (v == null) return;
    final t = Termin(a['titel'] as String, a['ort'] as String, DateTime.fromMillisecondsSinceEpoch(a['beginn'] as int));
    plan = Losgehplan(t, Ort.json(a['ziel'] as Map<String, dynamic>), v);
    await _weckerStellen(hintergrund: true);
  }

  /// Späteste Abfahrt, die noch reicht; bei Gleichstand weniger Umstiege (Liste ist danach sortiert).
  static Future<Verbindung?> _spaeteste(Api api, Map<String, dynamic> a) async {
    final von = (a['von'] as List).cast<num>(), ziel = a['ziel'] as Map;
    final (vs, _) = await api.route(von[0].toDouble(), von[1].toDouble(), (ziel['lat'] as num).toDouble(),
        (ziel['lon'] as num).toDouble(),
        ab: DateTime.fromMillisecondsSinceEpoch(a['bis'] as int),
        ankunft: true,
        kmh: (a['kmh'] as num).toDouble(),
        art: a['art'] as String);
    Verbindung? v;
    for (final x in vs) {
      if (v == null || x.ab.isAfter(v.ab)) v = x;
    }
    return v;
  }

  /// Meldung zur Losgehzeit − Vorlauf stellen, aus der App mit Nachrechnen vorher. Im Hintergrund meldet er sich
  /// sofort, wenn die neue Zeit schon vorbei ist, man aber noch los kann (z. B. Bus fährt früher).
  Future<void> _weckerStellen({bool hintergrund = false}) async {
    final p = plan, v = p?.v, jetzt = DateTime.now();
    if (p == null || v == null) return _kanal.invokeMethod('loeschen');
    var wann = v.ab.subtract(Duration(minutes: vorlauf));
    if (wann.isBefore(jetzt)) {
      if (!hintergrund || !v.ab.isAfter(jetzt)) return _kanal.invokeMethod('loeschen');
      wann = jetzt;
    }
    final rest = v.ab.difference(wann).inMinutes, vorher = wann.subtract(_nachrechnenVorher);
    await _kanal.invokeMethod('stellen', {
      'zeit': wann.millisecondsSinceEpoch,
      'titel': rest <= 0 ? 'Jetzt los: ${p.termin.titel}' : 'In $rest min los: ${p.termin.titel}',
      'text': losText(p),
      if (!hintergrund && vorher.isAfter(jetzt.add(const Duration(minutes: 1))))
        'nachrechnen': vorher.millisecondsSinceEpoch,
    });
  }
}

String _uhr(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';

/// „Los 9:32 · Linie 33 ab Ponttor 9:38 · an 9:55“ – für Startseite und Meldung.
String losText(Losgehplan p) {
  final v = p.v;
  if (p.ziel == null) return 'Ort „${p.termin.ort}“ nicht gefunden';
  if (v == null) return 'Keine Verbindung bis ${_uhr(p.termin.beginn)}';
  final erste = v.abschnitte.where((a) => a.fahrt).firstOrNull;
  return [
    'Los ${_uhr(v.ab)}',
    if (erste != null) 'Linie ${erste.linie} ab ${erste.von?.name ?? ''} ${_uhr(erste.abRt ?? erste.ab)}',
    'an ${_uhr(v.ankunft)}',
  ].join(' · ');
}

/// Einstellungen → Losgeh-Wecker: an/aus, Vorlauf der Meldung, Puffer vor Terminbeginn.
class LosgehSeite extends StatelessWidget {
  const LosgehSeite({super.key});

  @override
  Widget build(BuildContext context) {
    final l = Losgehen.instanz;
    Widget minuten(String titel, String unter, int wert, ValueChanged<int> neu) => ListTile(
          title: Text(titel),
          subtitle: Text(unter),
          trailing: DropdownButton<int>(
            value: wert,
            items: [for (final m in const [0, 2, 5, 10, 15, 20]) DropdownMenuItem(value: m, child: Text('$m min'))],
            onChanged: l.an ? (m) => neu(m!) : null,
          ),
        );
    return Scaffold(
      appBar: AppBar(title: const Text('Losgeh-Wecker')),
      body: ListenableBuilder(
        listenable: l,
        builder: (context, _) => ListView(children: [
          SwitchListTile(
            secondary: const Icon(Icons.alarm),
            title: const Text('Losgeh-Wecker'),
            subtitle: const Text('Nächster Termin mit Ort aus dem Kalender (heute, nächste 12 h): meldet sich, wenn du los '
                'musst. Termine bleiben auf dem Handy.'),
            value: l.an,
            onChanged: (v) async {
              if (!await l.setzen(an: v) && v && context.mounted) {
                ScaffoldMessenger.of(context)
                    .showSnackBar(const SnackBar(content: Text('Ohne Zugriff auf den Kalender geht es nicht')));
              }
            },
          ),
          minuten('Meldung vorher', 'so lange vor der Losgehzeit', l.vorlauf, (m) => l.setzen(vorlauf: m)),
          minuten('Puffer', 'so lange vor Terminbeginn ankommen', l.puffer, (m) => l.setzen(puffer: m)),
          const ListTile(
            leading: Icon(Icons.info_outline),
            subtitle: Text('Gerechnet wird, wenn du die App öffnest, und 10 min vor der Meldung noch einmal mit '
                'aktueller Echtzeit; die Meldung kommt auch bei geschlossener App. '
                'Sie ist lautlos – Ton und Vibration lassen sich in den Android-Einstellungen der App unter '
                'Benachrichtigungen → Losgeh-Wecker einschalten.'),
          ),
        ]),
      ),
    );
  }
}
