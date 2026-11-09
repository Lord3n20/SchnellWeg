// Das Profil lernt aus aufgezeichneten Routen (D-46). Kalibrieren setzt die Werte neu und läuft nur von Hand; hier wird nachgeführt:
// Tempo, Hang und Belag aus den selbst zurückgelegten Stücken, dazu wie Bus und Bahn genutzt werden (ab welcher Ersparnis, wie lange gewartet wird,
// welche Strecken). Ist eine erkannte Fahrt nicht sicher, bleibt das Profil unverändert. Ein ungeplanter Bus wird erst nach einer Rückfrage gelernt.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'api.dart';
import 'hang_messung.dart';
import 'messung.dart';
import 'standort.dart';
import 'tempo.dart';
import 'zugang.dart';

const _hinter = MethodChannel('schnellweg/messung/hinter');
const _lernMaxM = 30000.0; // so viele Meter trägt das Gelernte; neue Fahrten verdrängen alte
const _lernMinM = 300.0;
const _lernMinMs = 120000.0;
const _obsMax = 20, _wartenMax = 10;
const _obsMin = 3, _wartenMin = 5, _streckeMin = 3;

/// Einstieg für den Hintergrund (MessDienst startet dafür eine Flutter-Engine ohne Oberfläche).
@pragma('vm:entry-point')
Future<void> messungAbschliessen() async {
  WidgetsFlutterBinding.ensureInitialized();
  var r = ('Messung beendet', 'In der App unter Profile → Messen abschließen');
  try {
    r = await abschliessen(await Messung.datei());
  } catch (_) {
    // die Aufzeichnung bleibt liegen
  } finally {
    await _hinter.invokeMethod('fertig', {'titel': r.$1, 'text': r.$2});
  }
}

void ergebnisAnwenden(TempoProfil p, double kmh, HangErgebnis? e, {bool kalibriert = true}) {
  p.kmh = e != null && p.erlaubt(e.kmhEben) ? e.kmhEben : kmh;
  if (kalibriert) p.gemessen = DateTime.now();
  if (e == null) return;
  p.auf = e.auf ?? p.auf;
  p.ab = e.ab ?? p.ab;
  p.zuFussAb = e.zuFussAb ?? p.zuFussAb;
  e.belag.forEach((k, v) => v == null ? p.belagRegeln.remove(k) : p.belagRegeln[k] = v);
  if (kalibriert) p.lern = [for (final x in e.stuecke) [x.g, x.c, x.m, x.s]]; // neuer Ausgangspunkt
}

String messText(TempoProfil p, HangErgebnis? e) {
  if (e == null) return 'Tempo gespeichert. Hang und Belag ließen sich nicht auswerten, die Einstellungen bleiben.';
  if (e.auf == null && e.ab == null && e.zuFussAb == null && e.belag.isEmpty) return 'Tempo gespeichert. Für Hang und Belag war zu wenig dabei.';
  return 'Gemessen: eben ${tempoText(p.kmh)}, '
      '${p.zuFussAb > 0 ? 'zu Fuß ab ${p.zuFussAb} %' : 'bergauf ${p.auf} %'}, bergab ${p.ab > 0 ? '+' : ''}${p.ab} %'
      '${e.belag.isEmpty ? '' : ', ${e.belag.length} Beläge'}';
}

/// Fasst alte und neue Stücke zusammen; über [_lernMaxM] wird alles anteilig gekürzt, damit neue Fahrten mehr zählen.
List<List<num>> lernMischen(List<List<num>> alt, List<Stueck> neu) {
  final summe = <String, List<num>>{};
  void dazu(num g, num c, num m, num s) {
    final x = summe.putIfAbsent('${g.round()}/${c.round()}', () => [g, c, 0.0, 0.0]);
    x[2] = x[2] + m;
    x[3] = x[3] + s;
  }

  final gesamtAlt = alt.fold<double>(0, (a, x) => a + x[2]);
  final gesamtNeu = neu.fold<double>(0, (a, x) => a + x.m);
  final f = gesamtAlt > 0 && gesamtAlt + gesamtNeu > _lernMaxM ? ((_lernMaxM - gesamtNeu) / gesamtAlt).clamp(0.0, 1.0) : 1.0;
  for (final x in alt) {
    dazu(x[0], x[1], x[2] * f, x[3] * f);
  }
  for (final x in neu) {
    dazu(x.g, x.c, x.m, x.s);
  }
  return summe.values.toList();
}

/// Kleinste Schwelle in Minuten, die genutzte und ausgelassene Busse am besten trennt.
int busSchwelle(List<(double, bool)> obs) {
  var beste = 0, fehlerMin = 1 << 30;
  for (var t = 0; t <= 30; t++) {
    final fehler = obs.where((o) => o.$2 ? o.$1 < t : o.$1 >= t).length;
    if (fehler < fehlerMin) {
      fehlerMin = fehler;
      beste = t;
    }
  }
  return beste;
}

/// Trägt eine beobachtete Fahrt ein. [spart]: Minuten gegenüber dem eigenen Tempo; [bus]: zählt für die Bus-Schwelle.
void busEintragen(TempoProfil p, {required double spart, required bool genutzt, required bool bus, required String strecke, int? warteS}) {
  final l = p.busLern;
  if (bus) {
    final obs = [...(l['obs'] as List? ?? const []), [spart, genutzt ? 1 : 0]];
    l['obs'] = obs.length > _obsMax ? obs.sublist(obs.length - _obsMax) : obs;
    final o = [for (final x in l['obs'] as List) ((x[0] as num).toDouble(), x[1] == 1)];
    if (o.length >= _obsMin && p.meiden['bus'] != true && p.meiden['oev'] != true) {
      final t = busSchwelle(o);
      t == 0 ? p.meiden.remove('bus_min') : p.meiden['bus_min'] = t;
    }
  }
  if (genutzt && warteS != null) {
    final w = [...(l['warten'] as List? ?? const []), warteS];
    l['warten'] = w.length > _wartenMax ? w.sublist(w.length - _wartenMax) : w;
  }
  final st = Map<String, dynamic>.of(l['strecken'] as Map<String, dynamic>? ?? const {});
  final x = List<dynamic>.of(st[strecke] as List? ?? const [0, 0]);
  x[genutzt ? 0 : 1] = (x[genutzt ? 0 : 1] as int) + 1;
  st[strecke] = x;
  l['strecken'] = st;
}

/// Längste Wartezeit in Minuten, die das Profil hinnimmt; null = noch zu wenig gesehen.
int? warteGrenze(TempoProfil p) {
  final w = [for (final x in p.busLern['warten'] as List? ?? const []) (x as num).toInt()]..sort();
  if (w.length < _wartenMin) return null;
  return (w[(w.length * 0.9).floor().clamp(0, w.length - 1)] / 60).ceil() + 2;
}

(int, int) _strecke(TempoProfil p, Abschnitt a) {
  final x = (p.busLern['strecken'] as Map?)?['${a.von?.id}|${a.nach?.id}'] as List?;
  return x == null ? (0, 0) : (x[0] as int, x[1] as int);
}

bool _gemieden((int, int) s) => s.$2 >= _streckeMin && s.$2 > s.$1 * 2;
bool _beliebt((int, int) s) => s.$1 >= _streckeMin && s.$1 > s.$2;

/// Längste Wartezeit beim Umsteigen in einer Verbindung, ohne Fußwege dazwischen.
Duration umsteigeWarten(Verbindung v) {
  var groesste = Duration.zero;
  DateTime? an;
  var geh = Duration.zero;
  for (final a in v.abschnitte) {
    if (!a.fahrt) {
      geh += a.an.difference(a.ab);
      continue;
    }
    if (an != null) {
      final w = a.ab.difference(an) - geh;
      if (w > groesste) groesste = w;
    }
    an = a.an;
    geh = Duration.zero;
  }
  return groesste;
}

/// Wendet das Gelernte auf die Verbindungen an; es bleibt immer mindestens eine.
List<Verbindung> nachGelerntem(List<Verbindung> l, TempoProfil p) {
  if (!p.messbar || p.busLern.isEmpty || l.length < 2) return l;
  final grenze = warteGrenze(p);
  bool raus(Verbindung v) =>
      v.fahrten > 0 && ((grenze != null && umsteigeWarten(v).inMinutes > grenze) || v.abschnitte.any((a) => a.fahrt && _gemieden(_strecke(p, a))));
  final bleibt = l.where((v) => !raus(v)).toList();
  final r = bleibt.isEmpty ? l : bleibt;
  final frueh = r.map((v) => v.ankunft).reduce((a, b) => a.isBefore(b) ? a : b).add(const Duration(minutes: 3));
  bool vorn(Verbindung v) => !v.ankunft.isAfter(frueh) && v.abschnitte.any((a) => a.fahrt && _beliebt(_strecke(p, a)));
  return [...r.where(vorn), ...r.where((v) => !vorn(v))];
}

/// Zeilen für die Profilseite.
List<String> gelerntText(TempoProfil p) {
  final m = p.lern.fold<double>(0, (a, x) => a + x[2]);
  final o = (p.busLern['obs'] as List? ?? const []).length;
  final w = warteGrenze(p);
  final st = (p.busLern['strecken'] as Map? ?? const {}).length;
  return [
    if (m > 0) 'Tempo, Hang und Belag aus ${(m / 1000).toStringAsFixed(1).replaceAll('.', ',')} km',
    if (o > 0) o < _obsMin ? 'Bus: $o von $_obsMin Beobachtungen' : 'Bus lohnt ab ${p.meiden['bus_min'] ?? 0} min Ersparnis ($o Beobachtungen)',
    if (w != null) 'Umsteigen: bis $w min Wartezeit',
    if (st > 0) '$st Strecken gemerkt',
  ];
}

/// Beschreibt die gewählte Verbindung für die Aufzeichnung.
String routeKopf(Verbindung v, Map<String, Halt> halte) {
  double? meter(HaltRef? a, HaltRef? b) {
    final x = halte[a?.id], y = halte[b?.id];
    return x == null || y == null ? null : messAbstand(x.lat, x.lon, y.lat, y.lon) * 1.3;
  }

  return jsonEncode({
    'fahrten': [
      for (final a in v.abschnitte)
        if (a.fahrt)
          {
            'linie': a.linie,
            'von': a.von?.id,
            'nach': a.nach?.id,
            'ab': a.ab.millisecondsSinceEpoch ~/ 1000,
            'an': a.an.millisecondsSinceEpoch ~/ 1000,
            'm': ?meter(a.von, a.nach),
          }
    ],
  });
}

class LernFrage {
  final String profil, linie, von, nach, vonName, nachName;
  final double spart;
  final int warteS;
  final bool bus;
  LernFrage(this.profil, this.linie, this.von, this.nach, this.vonName, this.nachName, this.spart, this.warteS, this.bus);
  LernFrage.json(Map<String, dynamic> j)
      : profil = j['profil'] as String,
        linie = j['linie'] as String,
        von = j['von'] as String,
        nach = j['nach'] as String,
        vonName = j['von_name'] as String,
        nachName = j['nach_name'] as String,
        spart = (j['spart'] as num).toDouble(),
        warteS = j['warte_s'] as int,
        bus = j['bus'] as bool;
  Map<String, dynamic> toJson() => {
        'profil': profil, 'linie': linie, 'von': von, 'nach': nach, 'von_name': vonName, 'nach_name': nachName, 'spart': spart, 'warte_s': warteS, 'bus': bus,
      };
}

Future<File> _fragenDatei() async => File('${(await getApplicationSupportDirectory()).path}/lern_fragen.json');

Future<List<LernFrage>> lernFragen() async {
  try {
    final j = jsonDecode(await (await _fragenDatei()).readAsString()) as List;
    return [for (final x in j) LernFrage.json(x as Map<String, dynamic>)];
  } on Exception {
    return const [];
  }
}

Future<void> _fragenSpeichern(List<LernFrage> l) async {
  final f = await _fragenDatei();
  if (l.isEmpty) {
    if (await f.exists()) await f.delete();
    return;
  }
  await f.writeAsString(jsonEncode(l), flush: true);
}

/// Antwort auf eine Rückfrage; bei Nein bleibt das Profil, wie es ist.
Future<void> lernAntwort(LernFrage f, bool ja) async {
  if (ja) {
    final t = await Tempo.oeffnen();
    final p = t.profile.where((x) => x.name == f.profil).firstOrNull;
    if (p != null) {
      busEintragen(p, spart: f.spart, genutzt: true, bus: f.bus, strecke: '${f.von}|${f.nach}', warteS: f.warteS);
      await t.speichern();
    }
  }
  final rest = await lernFragen();
  rest.removeWhere((x) => x.von == f.von && x.nach == f.nach && x.linie == f.linie);
  await _fragenSpeichern(rest);
}

/// Wertet die Aufzeichnung aus; liefert Titel und Text der Meldung. Eine Kalibrierung, die nicht klappt, bleibt liegen.
Future<(String, String)> abschliessen(File datei) async {
  final ereignisse = await MessLeser(datei).neue();
  final kopf = ereignisse.whereType<MessKopf>().where((k) => k.art == 'k' || k.art == 'l').firstOrNull;
  if (kopf == null) return const ('Messung beendet', 'Nichts aufgezeichnet.');
  final t = await Tempo.oeffnen();
  final p = t.profile.where((x) => x.name == kopf.wert).firstOrNull;
  if (p == null) {
    await datei.delete();
    return const ('Messung beendet', 'Das Profil gibt es nicht mehr.');
  }
  final fuss = t.profile.where((x) => x.art == 'fuss').firstOrNull?.name;
  if (kopf.art == 'k') {
    final s = MessStand(p.art == 'fuss' ? (0.6, 3.0) : (1.0, 14.0));
    ereignisse.forEach(s.aufnehmen);
    if (!s.reicht) return ('Messung zu kurz', 'Unter Profile → Messen kannst du weitermessen.');
    final kmh = (s.kmh * 10).round() / 10;
    if (!p.erlaubt(kmh)) return ('Messung passt nicht', 'Gemessen ${tempoText(kmh)}. Unter Profile → Messen ansehen.');
    final e = await hangMessen(s.spur, kmh, eigen: p.eigen, fuss: fuss);
    if (e == null) return ('Noch nicht ausgewertet', 'Der Server war nicht erreichbar. Unter Profile → Messen noch einmal auswerten.');
    ergebnisAnwenden(p, kmh, e);
    await t.speichern();
    await datei.delete();
    return ('„${p.anzeige}“ kalibriert', messText(p, e));
  }
  final r = await routeLernen(ereignisse, p, fuss);
  if (r.$3) await t.speichern();
  await datei.delete();
  return (r.$1, r.$2);
}

Future<(String, String, bool)> routeLernen(List<Object> ereignisse, TempoProfil p, String? fuss) async {
  const nein = 'Dein Profil bleibt unverändert.';
  // Spur mit Uhrzeit
  var versatz = 0.0;
  final punkte = <(double, Fix)>[];
  var pause = false;
  for (final x in ereignisse) {
    if (x is MessUhr) versatz = x.uhr - x.zeit;
    if (x is MessPause) pause = x.an;
    if (x is Fix && !pause) punkte.add(((x.zeit + versatz) / 1000, x));
  }
  if (punkte.length < 10) return ('Route nicht ausgewertet', 'Zu wenig aufgezeichnet. $nein', false);
  final duenn = <List<double>>[];
  var zuletzt = 0.0;
  for (final (u, f) in punkte) {
    if (f.genau > 30 || u - zuletzt < 4) continue;
    zuletzt = u;
    duenn.add([double.parse(f.lat.toStringAsFixed(6)), double.parse(f.lon.toStringAsFixed(6)), u.roundToDouble(), double.parse(f.tempo.toStringAsFixed(1))]);
  }
  final List<Map<String, dynamic>> fahrten;
  final z = await Zugang.laden();
  if (z == null) return ('Route nicht ausgewertet', nein, false);
  final api = Api(z);
  try {
    fahrten = await api.fahrten(duenn);
  } on Exception {
    return ('Route nicht ausgewertet', 'Der Server konnte sie nicht prüfen. $nein', false);
  } finally {
    api.schliessen();
  }
  if (fahrten.any((f) => f['sicher'] != true)) return ('Route nicht gelernt', 'Unsicher, ob du unterwegs Bus oder Bahn gefahren bist. $nein', false);

  final kopf = ereignisse.whereType<MessKopf>().where((k) => k.art == 'r').firstOrNull;
  final geplant = kopf == null ? const <Map<String, dynamic>>[] : ((jsonDecode(kopf.wert) as Map)['fahrten'] as List).cast<Map<String, dynamic>>();
  final ms = p.kmh / 3.6;
  final fragen = <LernFrage>[];
  var geaendert = false, bus = 0;
  bool ueberlappt(Map<String, dynamic> a, Map<String, dynamic> b, int rand) => (a['ab'] as num) < (b['an'] as num) + rand && (b['ab'] as num) < (a['an'] as num) + rand;
  for (final f in fahrten) {
    final dauer = (f['an'] as num) - (f['ab'] as num), warte = (f['warte_s'] as num).toInt();
    final spart = ((f['m'] as num) / ms - dauer - warte) / 60;
    final vonId = (f['von'] as Map)['id'] as String, nachId = (f['nach'] as Map)['id'] as String;
    final plan = geplant.any((g) => g['linie'] == f['linie'] && ueberlappt(g, f, 300));
    if (plan) {
      busEintragen(p, spart: spart, genutzt: true, bus: f['bus'] == true, strecke: '$vonId|$nachId', warteS: warte);
      geaendert = true;
      bus++;
    } else {
      fragen.add(LernFrage(p.name, f['linie'] as String, vonId, nachId, (f['von'] as Map)['name'] as String, (f['nach'] as Map)['name'] as String, spart, warte, f['bus'] == true));
    }
  }
  // Geplante Fahrt, die nicht stattfand, weil selbst gefahren wurde
  for (final g in geplant) {
    if (fahrten.any((f) => ueberlappt(g, f, 300))) continue;
    final im = punkte.where((x) => x.$1 >= (g['ab'] as num) && x.$1 <= (g['an'] as num)).toList();
    final dauer = (g['an'] as num) - (g['ab'] as num);
    if (g['m'] == null || dauer <= 0 || im.length < dauer * 0.6 || im.where((x) => x.$2.tempo >= 1).length < im.length * 0.6) continue; // nicht klar
    final spart = ((g['m'] as num) / ms - dauer) / 60;
    busEintragen(p, spart: spart, genutzt: false, bus: RegExp(r'^\d').hasMatch(g['linie'] as String), strecke: '${g['von']}|${g['nach']}');
    geaendert = true;
  }
  // Tempo, Hang und Belag aus dem, was selbst zurückgelegt wurde
  final rollt = p.eigen && p.kmh >= 7;
  final s = MessStand(p.art == 'fuss' ? (0.6, 3.0) : (rollt ? 1.6 : 1.0, 14.0));
  var drin = false;
  for (final (u, f) in punkte) {
    final jetzt = fahrten.any((x) => u >= (x['ab'] as num) - 30 && u <= (x['an'] as num) + 30);
    if (jetzt) {
      if (!drin) s.luecke();
    } else {
      s.aufnehmen(f);
    }
    drin = jetzt;
  }
  var text = '';
  if (s.strecke >= _lernMinM && s.gehMs >= _lernMinMs) {
    final st = await stueckeHolen(s.spur);
    if (st != null && st.isNotEmpty) {
      p.lern = lernMischen(p.lern, st);
      final alle = [for (final x in p.lern) Stueck(x[0].toDouble(), x[2].toDouble(), x[3].toDouble(), x[1].toInt())];
      final e = hangAuswerten(alle, p.kmh, eigen: p.eigen, fuss: fuss);
      ergebnisAnwenden(p, p.erlaubt(e.kmhEben) ? e.kmhEben : p.kmh, e, kalibriert: false);
      geaendert = true;
      text = 'Eben jetzt ${tempoText(p.kmh)}. ';
    }
  }
  if (fragen.isNotEmpty) await _fragenSpeichern([...await lernFragen(), ...fragen]);
  if (bus > 0) text += '$bus Fahrt${bus == 1 ? '' : 'en'} gelernt. ';
  if (fragen.isNotEmpty) text += 'Tippe, um ${fragen.length == 1 ? 'eine Frage' : '${fragen.length} Fragen'} zu beantworten.';
  if (!geaendert && fragen.isEmpty) return ('Route ausgewertet', 'Zu wenig eigene Strecke. $nein', false);
  return ('„${p.anzeige}“ verbessert', text.trim(), geaendert);
}
