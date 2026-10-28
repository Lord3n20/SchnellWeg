// Kartendateien auf dem Handy (D-20): Übersicht (ganz DE grob) lädt die App selbst, Bundesländer auf Wunsch.
// Alles liegt im App-Ordner (karten/<id>.pmtiles), Kacheln werden nie einzeln übers Netz geholt.
// Höchstens einmal am Tag fragt die App die Liste und – für vorhandene Dateien – neue Stände ab.
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:pmtiles/pmtiles.dart';
import 'package:vector_map_tiles/vector_map_tiles.dart';
import 'package:vector_map_tiles_pmtiles/vector_map_tiles_pmtiles.dart';

import 'api.dart';

const _pruefTakt = Duration(days: 1);
const _versuche = 8; // Netzabbrüche während eines Downloads: so oft nach kurzer Pause weitermachen
const _dienstKanal = MethodChannel('schnellweg/karte'); // KartenDienst.kt: Download läuft im Hintergrund weiter
const uebersicht = 'uebersicht';

class KartenLager extends ChangeNotifier {
  KartenLager._();
  static final instanz = KartenLager._();

  Api? _api;
  Directory? _ordner;
  List<KartenRegion> regionen = []; // Angebot des Servers (zuletzt bekannt)
  final Map<String, PmTilesArchive> _archive = {};
  final Map<String, double> laedt = {}; // id → Fortschritt 0..1, -1 = unbekannt
  String? fehler;
  int stand = 0; // ändert sich, wenn Dateien dazukommen oder ersetzt werden
  final Set<String> _gewollt = {}; // Downloads, die jemand angestoßen hat (nicht die stille Prüfung auf neue Stände)
  String? _gemeldet; // zuletzt an den Dienst gemeldeter Text

  bool hat(String id) => _archive.containsKey(id);
  bool get bereit => _archive.isNotEmpty;

  VectorTileProvider? get uebersichtQuelle {
    final a = _archive[uebersicht];
    return a == null ? null : PmTilesVectorTileProvider.fromArchive(a);
  }

  /// Alle geladenen Bundesländer als eine Quelle; null, solange keins da ist.
  VectorTileProvider? get regionenQuelle {
    final l = [
      for (final r in regionen)
        if (r.id != uebersicht && _archive[r.id] != null) (r, _archive[r.id]!)
    ];
    return l.isEmpty ? null : _Regionen(l);
  }

  /// Bundesland an dieser Stelle, das noch fehlt (und keine geladene Region deckt die Stelle ab).
  KartenRegion? fehltBei(double lat, double lon) {
    KartenRegion? fehlt;
    for (final r in regionen) {
      if (r.id == uebersicht || !r.enthaelt(lat, lon)) continue;
      if (hat(r.id)) return null;
      fehlt ??= r;
    }
    return fehlt;
  }

  Future<File> _datei(String id) async => File('${(await _ordnerHolen()).path}/$id.pmtiles');

  Future<Directory> _ordnerHolen() async =>
      _ordner ??= await Directory('${(await getApplicationSupportDirectory()).path}/karten').create(recursive: true);

  /// Vorhandenes sofort öffnen, dann (höchstens täglich) Liste und Stände prüfen; die Übersicht lädt sie selbst.
  Future<void> start(Api api) async {
    _api = api;
    final ordner = await _ordnerHolen();
    final alt = File('${ordner.parent.path}/karte.pmtiles'); // Raum Aachen vor D-20
    for (final f in [alt, File('${alt.path}.geprueft'), File('${alt.path}.part')]) {
      if (await f.exists()) await f.delete();
    }
    final liste = File('${ordner.path}/regionen.json');
    if (await liste.exists()) {
      try {
        regionen = [
          for (final r in (jsonDecode(await liste.readAsString()) as List).cast<Map<String, dynamic>>())
            KartenRegion.json(r)
        ];
      } on FormatException {
        regionen = [];
      }
    }
    await for (final f in ordner.list()) {
      if (f is File && f.path.endsWith('.pmtiles')) {
        final id = f.uri.pathSegments.last.replaceAll('.pmtiles', '');
        await _oeffnen(id, f);
      }
    }
    notifyListeners();

    final geprueft = File('${ordner.path}/.geprueft');
    if (regionen.isNotEmpty &&
        await geprueft.exists() &&
        DateTime.now().difference(await geprueft.lastModified()) < _pruefTakt &&
        hat(uebersicht)) {
      return;
    }
    try {
      regionen = await api.kartenRegionen();
      await liste.writeAsString(jsonEncode([for (final r in regionen) r.toJson()]));
      fehler = null;
      notifyListeners();
      if (!hat(uebersicht)) await laden(uebersicht);
      for (final id in _archive.keys.toList()) {
        await laden(id, nurNeu: true);
      }
      await geprueft.writeAsString('');
    } on ApiFehler catch (e) {
      fehler = 'Kartenliste nicht geladen ($e)';
      notifyListeners();
    }
  }

  /// Datei [id] laden (oder mit [nurNeu] nur bei neuerem Serverstand ersetzen). Ein Download läuft im Hintergrund
  /// weiter (KartenDienst) und setzt nach Netzabbrüchen selbst wieder an.
  Future<void> laden(String id, {bool nurNeu = false}) async {
    final api = _api;
    if (api == null || laedt.containsKey(id)) return;
    final f = await _datei(id);
    laedt[id] = nurNeu ? -1 : 0;
    if (!nurNeu) {
      _gewollt.add(id);
      notifyListeners();
    }
    var zuletzt = DateTime(0);
    try {
      for (var versuch = 1;; versuch++) {
        final da = await f.exists();
        try {
          final neu = await api.karte(id, f, da ? await f.lastModified() : null, fortschritt: (n, gesamt) {
            final jetzt = DateTime.now();
            if (jetzt.difference(zuletzt) < const Duration(milliseconds: 300)) return;
            zuletzt = jetzt;
            laedt[id] = gesamt > 0 ? n / gesamt : -1;
            notifyListeners();
          });
          if (neu) await _oeffnen(id, f);
          fehler = null;
          break;
        } on ApiFehler catch (e) {
          if (nurNeu || e.art != 'netz' || versuch >= _versuche) rethrow;
          await Future<void>.delayed(Duration(seconds: 5 * versuch)); // .part bleibt, es geht per Range weiter
        }
      }
    } on ApiFehler catch (e) {
      if (!nurNeu) fehler = 'Karte nicht geladen ($e)';
    } finally {
      laedt.remove(id);
      _gewollt.remove(id);
      notifyListeners();
    }
  }

  @override
  void notifyListeners() {
    super.notifyListeners();
    _dienst();
  }

  /// Hält den KartenDienst an, solange ein gewollter Download läuft, und zeigt dort den Fortschritt.
  void _dienst() {
    final ids = _gewollt.where(laedt.containsKey).toList();
    if (ids.isEmpty) {
      if (_gemeldet != null) _dienstKanal.invokeMethod<void>('beenden').ignore();
      _gemeldet = null;
      return;
    }
    final name = regionen.where((r) => r.id == ids.first).firstOrNull?.name ?? 'Übersicht';
    final p = laedt[ids.first]!;
    final prozent = p >= 0 ? (p * 100).floor() : -1;
    final text = ids.length == 1 ? 'Karte $name wird geladen' : '${ids.length} Karten werden geladen';
    if ('$text $prozent' == _gemeldet) return;
    _gemeldet = '$text $prozent';
    _dienstKanal.invokeMethod<void>('melden', {'text': text, 'prozent': prozent}).ignore();
  }

  Future<void> loeschen(String id) async {
    final a = _archive.remove(id);
    stand++;
    notifyListeners();
    await Future<void>.delayed(const Duration(seconds: 2)); // laufende Kachelzugriffe auslaufen lassen
    await a?.close();
    final f = await _datei(id);
    for (final x in [f, File('${f.path}.part'), File('${f.path}.part.stand')]) {
      if (await x.exists()) await x.delete();
    }
  }

  /// Belegter Platz auf dem Handy in Bytes.
  Future<int> belegt() async {
    var n = 0;
    await for (final f in (await _ordnerHolen()).list()) {
      if (f is File) n += await f.length();
    }
    return n;
  }

  Future<void> _oeffnen(String id, File f) async {
    final PmTilesArchive neu;
    try {
      neu = await PmTilesArchive.from(f.path);
    } catch (_) {
      await f.delete(); // kaputte Datei: beim nächsten Mal neu laden
      return;
    }
    final alt = _archive[id];
    _archive[id] = neu;
    stand++;
    notifyListeners();
    if (alt != null) {
      await Future<void>.delayed(const Duration(seconds: 2));
      await alt.close();
    }
  }
}

/// Mehrere Bundesländer als eine Quelle: die erste Datei, deren Rechteck die Kachel berührt und die sie hat.
class _Regionen extends VectorTileProvider {
  final List<(KartenRegion, PmTilesArchive)> _l;
  _Regionen(this._l);

  @override
  int get maximumZoom => _l.map((e) => e.$2.maxZoom).reduce(max);

  @override
  int get minimumZoom => _l.map((e) => e.$2.minZoom).reduce(min);

  @override
  Future<Uint8List> provide(TileIdentity tile) async {
    final n = 1 << tile.z;
    final west = tile.x / n * 360 - 180, ost = (tile.x + 1) / n * 360 - 180;
    double breite(int y) => atan((exp(pi * (1 - 2 * y / n)) - exp(-pi * (1 - 2 * y / n))) / 2) * 180 / pi;
    final nord = breite(tile.y), sued = breite(tile.y + 1);
    final id = ZXY(tile.z, tile.x, tile.y).toTileId();
    for (final (r, a) in _l) {
      if (ost < r.west || west > r.ost || nord < r.sued || sued > r.nord) continue;
      try {
        return Uint8List.fromList((await a.tile(id)).bytes());
      } on TileNotFoundException {
        continue;
      }
    }
    throw ProviderException(message: 'Kachel in keiner Region: $tile', retryable: Retryable.none, statusCode: 404);
  }
}
