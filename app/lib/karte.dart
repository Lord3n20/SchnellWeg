// Karte offline aus den PMTiles-Dateien im KartenLager (D-20): Übersicht unten, geladene Bundesländer darüber.
// Fehlt an der angezeigten Stelle das Bundesland, bietet ein Hinweis oben das Laden an.
// Gezeichnet wird alles auf dem Handy: eigener Standort, Halte in der Nähe, gewählte Verbindung.
import 'package:flutter/material.dart';
import 'package:flutter_map/flutter_map.dart';
import 'package:latlong2/latlong.dart';
import 'package:vector_map_tiles/vector_map_tiles.dart';
import 'package:vector_map_tiles_pmtiles/vector_map_tiles_pmtiles.dart';
// ignore: implementation_imports
import 'package:vector_map_tiles_pmtiles/src/themes/v4/_package.dart' as v4;

import 'api.dart';
import 'kartenlager.dart';

const _detailAb = 10.0; // ab dieser Zoomstufe fällt die grobe Übersicht auf
final _aachen = LatLng(50.7753, 6.0839); // Hbf, bis der Standort da ist

/// Protomaps-Stile mit vereinfachten Beschriftungen: die Originale bauen den Text mit `format` (zweisprachig,
/// Schrift je Schriftsystem), das der Renderer nicht kennt – dann fiele jede Beschriftung weg.
/// Stattdessen deutscher Name, sonst der Name vor Ort. Glyphen/Sprites werden nicht von außen geladen.
List<Map<String, Object>> _stil(List<Map<String, Object>> ebenen) => [
      for (final e in ebenen)
        if (e['layout'] case final Map<String, Object> l when l.containsKey('text-field'))
          {
            ...e,
            'layout': {
              ...l,
              'text-field': ['coalesce', ['get', 'name:de'], ['get', 'name']],
            },
          }
        else
          e,
    ];

final hellerStil = ProtomapsThemes().build(_stil(v4.themeLight)), _dunkel = ProtomapsThemes().build(_stil(v4.themeDark));

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
  final LatLng? standort, ziel;
  final List<LatLng> halte;
  final List<Strecke> strecken;
  final void Function(LatLng)? onAntippen; // Punkt als Ziel übernehmen
  const Karte({
    super.key,
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
  final _lager = KartenLager.instanz;
  bool _bereit = false; // FlutterMap gebaut, Kamera darf bewegt werden
  bool _folgen = true; // Ausschnitt passt sich an, bis der Nutzer selbst verschiebt
  KartenRegion? _fehlt; // Bundesland in der Kartenmitte, das noch nicht geladen ist

  @override
  void initState() {
    super.initState();
    _lager.addListener(_lagerGeaendert);
  }

  @override
  void didUpdateWidget(Karte alt) {
    super.didUpdateWidget(alt);
    if (alt.strecken != widget.strecken || alt.ziel != widget.ziel) _folgen = true;
    if (_folgen) WidgetsBinding.instance.addPostFrameCallback((_) => _ausschnitt());
  }

  @override
  void dispose() {
    _lager.removeListener(_lagerGeaendert);
    super.dispose();
  }

  void _lagerGeaendert() {
    if (!mounted) return;
    setState(() {});
    if (_bereit) _pruefeRegion(_kamera.camera);
  }

  void _pruefeRegion(MapCamera c) {
    final f = c.zoom >= _detailAb ? _lager.fehltBei(c.center.latitude, c.center.longitude) : null;
    if (f?.id != _fehlt?.id) setState(() => _fehlt = f);
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
    final grob = _lager.uebersichtQuelle, fein = _lager.regionenQuelle;
    if (grob == null && fein == null) {
      final p = _lager.laedt[uebersicht];
      return Container(
        color: farbe.surfaceContainerHighest,
        alignment: Alignment.center,
        padding: const EdgeInsets.all(16),
        child: Text(
            _lager.fehler ?? (p == null ? 'Karte wird geöffnet …' : 'Übersichtskarte wird geladen … ${p >= 0 ? '${(p * 100).round()} %' : ''}'),
            textAlign: TextAlign.center,
            style: TextStyle(color: farbe.onSurfaceVariant)),
      );
    }
    final stil = Theme.of(context).brightness == Brightness.dark ? _dunkel : hellerStil;
    return Stack(children: [
      FlutterMap(
        mapController: _kamera,
        options: MapOptions(
          initialCenter: widget.standort ?? _aachen,
          initialZoom: 16,
          minZoom: 5,
          maxZoom: 19,
          backgroundColor: farbe.surfaceContainerHighest,
          interactionOptions: const InteractionOptions(flags: InteractiveFlag.all & ~InteractiveFlag.rotate),
          onMapReady: () {
            _bereit = true;
            _ausschnitt();
            _pruefeRegion(_kamera.camera);
          },
          onTap: widget.onAntippen == null ? null : (_, p) => widget.onAntippen!(p),
          onPositionChanged: (c, vomNutzer) {
            if (vomNutzer) _folgen = false;
            _pruefeRegion(c);
          },
        ),
        children: [
          if (grob != null)
            VectorTileLayer(
              key: ValueKey('grob${_lager.stand}'),
              theme: stil,
              tileProviders: TileProviders({'protomaps': grob}),
              fileCacheTtl: Duration.zero, // Quelle liegt ohnehin lokal
            ),
          if (fein != null)
            VectorTileLayer(
              key: ValueKey('fein${_lager.stand}'),
              theme: stil,
              tileProviders: TileProviders({'protomaps': fein}),
              fileCacheTtl: Duration.zero,
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
      ),
      if (_fehlt != null) Positioned(top: 8, left: 8, right: 8, child: _RegionLaden(_fehlt!)),
    ]);
  }
}

/// „Karte für Bayern laden (1,2 GB)“ – oder der Fortschritt, solange sie lädt.
class _RegionLaden extends StatelessWidget {
  final KartenRegion r;
  const _RegionLaden(this.r);

  @override
  Widget build(BuildContext context) {
    final lager = KartenLager.instanz;
    final p = lager.laedt[r.id];
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 4, 4, 4),
        child: Row(children: [
          Expanded(
            child: p == null
                ? Text('${r.name}: nur Übersicht', style: const TextStyle(fontWeight: FontWeight.w600))
                : Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text('${r.name} wird geladen …'),
                    const SizedBox(height: 4),
                    LinearProgressIndicator(value: p >= 0 ? p : null),
                  ]),
          ),
          if (p == null)
            TextButton.icon(
              onPressed: () => lager.laden(r.id),
              icon: const Icon(Icons.download),
              label: Text('Laden (${groesse(r.bytes)})'),
            )
          else
            const SizedBox(width: 12),
        ]),
      ),
    );
  }
}

String groesse(int bytes) => bytes >= 1 << 30
    ? '${(bytes / (1 << 30)).toStringAsFixed(1).replaceAll('.', ',')} GB'
    : '${(bytes / (1 << 20)).round()} MB';

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
