import 'package:material_ui/material_ui.dart';
import 'package:mpv_audio_kit/mpv_audio_kit.dart' show MpvAudioKit;

import 'start_screen.dart';

/// Инициализирует Flutter и нативный движок воспроизведения.
void main() {
  WidgetsFlutterBinding.ensureInitialized();
  MpvAudioKit.ensureInitialized();
  runApp(
    MaterialApp(
      title: 'Somnium Shade Cast',
      theme: ThemeData(colorSchemeSeed: Colors.indigo),
      home: const StartScreen(),
    ),
  );
}
