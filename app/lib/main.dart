import 'package:flutter/material.dart';

import 'controller.dart';
import 'screens/home_screen.dart';
import 'settings.dart';

void main() {
  runApp(LifeAgentApp(controller: AppController(PlatformStorage())..load()));
}

class LifeAgentApp extends StatelessWidget {
  const LifeAgentApp({super.key, required this.controller});

  final AppController controller;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Жизнь',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(colorSchemeSeed: Colors.teal, useMaterial3: true),
      darkTheme: ThemeData(colorSchemeSeed: Colors.teal, useMaterial3: true, brightness: Brightness.dark),
      home: HomeRoot(controller: controller),
    );
  }
}

/// Waits for the stored settings, then shows the home screen.
class HomeRoot extends StatelessWidget {
  const HomeRoot({super.key, required this.controller, this.now});

  final AppController controller;
  final DateTime Function()? now;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) => controller.loaded
          ? HomeScreen(controller: controller, now: now)
          : const Scaffold(body: Center(child: CircularProgressIndicator())),
    );
  }
}
