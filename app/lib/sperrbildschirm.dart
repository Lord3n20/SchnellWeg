// Seiten, die wie bei Öffi/Organic Maps über dem Sperrbildschirm erscheinen dürfen: Handy aufwecken zeigt sie
// sofort, ohne Entsperren. Nur solange die Seite offen ist; danach braucht die App wieder die Entsperrung.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

const _kanal = MethodChannel('schnellweg/sperrbildschirm');

class UeberSperrbildschirm extends StatefulWidget {
  final Widget child;
  const UeberSperrbildschirm({super.key, required this.child});

  @override
  State<UeberSperrbildschirm> createState() => _UeberSperrbildschirmState();
}

class _UeberSperrbildschirmState extends State<UeberSperrbildschirm> {
  @override
  void initState() {
    super.initState();
    _kanal.invokeMethod('zeigen', true).ignore();
  }

  @override
  void dispose() {
    _kanal.invokeMethod('zeigen', false).ignore();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
