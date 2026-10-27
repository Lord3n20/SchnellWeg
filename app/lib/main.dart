import 'package:flutter/material.dart';

import 'start.dart';

void main() => runApp(const SchnellWeg());

class SchnellWeg extends StatelessWidget {
  const SchnellWeg({super.key});

  @override
  Widget build(BuildContext context) {
    const saat = Color(0xFF00549F); // RWTH-Blau
    return MaterialApp(
      title: 'SchnellWeg',
      theme: ThemeData(colorSchemeSeed: saat),
      darkTheme: ThemeData(colorSchemeSeed: saat, brightness: Brightness.dark),
      home: const Start(),
    );
  }
}
