import 'package:flutter/material.dart';

import 'darstellung.dart';
import 'losgehen.dart' as losgehen;
import 'start.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await Darstellung.instanz.laden();
  runApp(const SchnellWeg());
}

/// Hintergrund-Einstieg des Losgeh-Weckers (muss in der Hauptbibliothek liegen, damit die Engine ihn findet).
@pragma('vm:entry-point')
Future<void> losgehNachrechnen() => losgehen.losgehNachrechnen();

class SchnellWeg extends StatelessWidget {
  const SchnellWeg({super.key});

  @override
  Widget build(BuildContext context) {
    const saat = Color(0xFF00549F); // RWTH-Blau
    final d = Darstellung.instanz;
    return ListenableBuilder(
      listenable: d,
      builder: (context, _) => MaterialApp(
        title: 'SchnellWeg',
        theme: ThemeData(colorSchemeSeed: saat),
        darkTheme: dunklesThema(saat, oled: d.oled),
        themeMode: d.modus,
        navigatorObservers: [seitenBeobachter],
        home: const Start(),
      ),
    );
  }
}
