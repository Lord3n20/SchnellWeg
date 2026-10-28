// Karte offline aus der PMTiles-Datei vom Server (/v1/map, einmal geladen, danach höchstens täglich auf Änderung
// geprüft). Gezeichnet wird alles auf dem Handy: eigener Standort, Halte in der Nähe, gewählte Verbindung.
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';
import 'package:path_provider/path_provider.dart';
import 'package:vector_map_tiles/vector_map_tiles.dart';
import 'package:vector_map_tiles_pmtiles/vector_map_tiles_pmtiles.dart';

import 'api.dart';

const _pruefTakt = Duration(days: 1);
final _aachen = LatLng(50.7753, 6.0839); // Hbf, bis der Standort da ist

/// Ablage der Kartendatei im App-Ordner (wird bei Deinstallation mit entfernt).
class KartenDatei {
  static Future<File> datei() async => File('${(await getApplicationSupportDirectory()).path}/karte.pmtiles');

  /// Holt die Karte, wenn sie fehlt oder die letzte Prüfung über einen Tag her ist. true = neue Datei.
  static Future<bool> sichern(Api api, {void Function(int, int)? fortschritt}) async {
    final f = await datei();
    final geprueft = File('${f.path}.geprueft');
    final da = await f.exists();
    if (da && await geprueft.exists() && DateTime.now().difference(await geprueft.lastModified()) < _pruefTakt) {
      return false;
    }
    final neu = await api.karte(f, da ? await f.lastModified() : null, fortschritt: fortschritt);
    await geprueft.writeAsString('');
    return neu;
  }
}

/// Ein Stück der Verbindung auf der Karte.
class Strecke {
  final List<LatLng> punkte;
  final bool fuss;
  final String linie;
  final List<LatLng> zwischen; // Fahrt: Zwischenhalte als kleine Punkte
  Strecke(this.punkte, this.fuss, this.linie, {this.zwischen = const []});
}

/// Linienfarbe, stabil je Liniennummer.
Color linienFarbe(String linie) {
  const farben = [
    Color(0xFF00549F), Color(0xFFCC071E), Color(0xFF57AB27), Color(0xFFF6A800),
    Color(0xFF7A6FAC), Color(0xFF0098A1), Color(0xFFE30066), Color(0xFF8D6E63),
  ];
  return farben[linie.codeUnits.fold<int>(7, (h, c) => (h * 31 + c) & 0x7fffffff) % farben.length];
}

class Karte extends StatefulWidget {
  final File? datei; // null = noch nicht geladen
  final int stand; // ändert sich, wenn die Datei ersetzt wurde
  final String? hinweis; // statt Karte, solange keine Datei da ist
  final LatLng? standort, ziel;
  final List<LatLng> halte;
  final List<Strecke> strecken;
  final void Function(LatLng)? onAntippen; // Punkt als Ziel übernehmen
  const Karte({
    super.key,
    required this.datei,
    required this.stand,
    this.hinweis,
    this.standort,
    this.ziel,
    this.halte = const [],
    this.strecken = const [],
    this.onAntippen,
  });

  @override
  State<Karte> createState() => _KarteState();
}

class _KarteState extends State<Karte> {
  final _kamera = MapController();
  PmTilesVectorTileProvider? _quelle;
  int _quelleStand = -1;
  bool _bereit = false; // FlutterMap gebaut, Kamera darf bewegt werden
  bool _folgen = true; // Ausschnitt passt sich an, bis der Nutzer selbst verschiebt

  @override
  void initState() {
    super.initState();
    _oeffnen();
  }

  @override
  void didUpdateWidget(Karte alt) {
    super.didUpdateWidget(alt);
    if (widget.stand != _quelleStand) _oeffnen();
    if (alt.strecken != widget.strecken || alt.ziel != widget.ziel) _folgen = true;
    if (_folgen) WidgetsBinding.instance.addPostFrameCallback((_) => _ausschnitt());
  }

  @override
  void dispose() {
    _quelle?.archive.close();
    super.dispose();
  }

  Future<void> _oeffnen() async {
    final f = widget.datei, stand = widget.stand;
    if (f == null || !await f.exists()) return;
    final q = await PmTilesVectorTileProvider.fromSource(f.path);
    if (!mounted) return q.archive.close();
    final alt = _quelle;
    setState(() {
      _quelle = q;
      _quelleStand = stand;
    });
    await alt?.archive.close();
  }

  void _ausschnitt() {
    if (!_bereit || !mounted) return;
    final punkte = [
      for (final s in widget.strecken) ...s.punkte,
      ?widget.standort,
      ?widget.ziel,
    ];
    if (widget.strecken.isEmpty && widget.ziel == null) {
      if (widget.standort != null) _kamera.move(widget.standort!, 16);
      return;
    }
    if (punkte.length < 2) return;
    _kamera.fitCamera(CameraFit.coordinates(
        coordinates: punkte, padding: const EdgeInsets.all(36), maxZoom: 17));
  }

  @override
  Widget build(BuildContext context) {
    final farbe = Theme.of(context).colorScheme;
    final q = _quelle;
    if (q == null) {
      return Container(
        color: farbe.surfaceContainerHighest,
        alignment: Alignment.center,
        padding: const EdgeInsets.all(16),
        child: Text(widget.hinweis ?? 'Karte wird geöffnet …',
            textAlign: TextAlign.center, style: TextStyle(color: farbe.onSurfaceVariant)),
      );
    }
    final dunkel = Theme.of(context).brightness == Brightness.dark;
    return FlutterMap(
      mapController: _kamera,
      options: MapOptions(
        initialCenter: widget.standort ?? _aachen,
        initialZoom: 16,
        minZoom: 8,
        maxZoom: 19,
        backgroundColor: farbe.surfaceContainerHighest,
        interactionOptions: const InteractionOptions(flags: InteractiveFlag.all & ~InteractiveFlag.rotate),
        onMapReady: () {
          _bereit = true;
          _ausschnitt();
        },
        onTap: widget.onAntippen == null ? null : (_, p) => widget.onAntippen!(p),
        onPositionChanged: (_, vomNutzer) {
          if (vomNutzer) _folgen = false;
        },
      ),
      children: [
        VectorTileLayer(
          key: ValueKey(_quelleStand),
          theme: dunkel ? ProtomapsThemes.darkV4() : ProtomapsThemes.lightV4(),
          tileProviders: TileProviders({'protomaps': q}),
          fileCacheTtl: Duration.zero, // Quelle liegt ohnehin lokal
        ),
        PolylineLayer(polylines: [
          for (final s in widget.strecken)
            Polyline(
              points: s.punkte,
              strokeWidth: s.fuss ? 4 : 6,
              color: s.fuss ? farbe.outline : linienFarbe(s.linie),
              pattern: s.fuss ? StrokePattern.dashed(segments: const [8, 6]) : const StrokePattern.solid(),
              borderStrokeWidth: s.fuss ? 0 : 1.5,
              borderColor: farbe.surface,
            ),
        ]),
        CircleLayer(circles: [
          if (widget.strecken.isEmpty)
            for (final h in widget.halte)
              CircleMarker(point: h, radius: 5, color: farbe.surface, borderColor: farbe.primary, borderStrokeWidth: 2),
          for (final s in widget.strecken)
            for (final p in s.zwischen)
              CircleMarker(point: p, radius: 2.5, color: farbe.surface, borderColor: linienFarbe(s.linie), borderStrokeWidth: 1.5),
          // Umstiege und Ein-/Ausstiege
          for (final s in widget.strecken.where((s) => !s.fuss))
            for (final p in [s.punkte.first, s.punkte.last])
              CircleMarker(point: p, radius: 5, color: farbe.surface, borderColor: linienFarbe(s.linie), borderStrokeWidth: 3),
          if (widget.standort != null) ...[
            CircleMarker(point: widget.standort!, radius: 11, color: farbe.primary.withValues(alpha: 0.2)),
            CircleMarker(point: widget.standort!, radius: 6, color: farbe.primary, borderColor: Colors.white, borderStrokeWidth: 2),
          ],
        ]),
        if (widget.ziel != null)
          MarkerLayer(markers: [
            Marker(
              point: widget.ziel!,
              width: 36,
              height: 36,
              alignment: Alignment.topCenter,
              child: Icon(Icons.place, size: 36, color: farbe.error),
            ),
          ]),
        const _Hinweis(),
      ],
    );
  }
}

/// Quellenangabe (ODbL/Protomaps), klein unten rechts.
class _Hinweis extends StatelessWidget {
  const _Hinweis();

  @override
  Widget build(BuildContext context) => Align(
        alignment: Alignment.bottomRight,
        child: Container(
          margin: const EdgeInsets.all(4),
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
          color: Theme.of(context).colorScheme.surface.withValues(alpha: 0.7),
          child: const Text('© OpenStreetMap · Protomaps', style: TextStyle(fontSize: 10)),
        ),
      );
}
