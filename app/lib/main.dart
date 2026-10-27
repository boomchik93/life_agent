import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'background.dart';
import 'controller.dart';
import 'notifications.dart';
import 'screens/home_screen.dart';
import 'settings.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final storage = PlatformStorage();
  try {
    await WorkmanagerScheduler.init();
    // The system keeps the schedule, but a reinstall or an update may drop it.
    await applyBackground(storage, WorkmanagerScheduler());
  } catch (_) {
    // Background work is optional: the app works without it.
  }
  runApp(LifeAgentApp(controller: AppController(storage)..load()));
}

class LifeAgentApp extends StatelessWidget {
  const LifeAgentApp({super.key, required this.controller});

  final AppController controller;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Жизнь',
      debugShowCheckedModeBanner: false,
      locale: const Locale('ru'),
      supportedLocales: const [Locale('ru')],
      localizationsDelegates: GlobalMaterialLocalizations.delegates,
      theme: ThemeData(colorSchemeSeed: Colors.teal, useMaterial3: true),
      darkTheme: ThemeData(colorSchemeSeed: Colors.teal, useMaterial3: true, brightness: Brightness.dark),
      home: HomeRoot(controller: controller),
    );
  }
}

/// Waits for the stored settings, then shows the home screen.
class HomeRoot extends StatelessWidget {
  const HomeRoot({super.key, required this.controller, this.now, this.scheduler, this.notifier});

  final AppController controller;
  final DateTime Function()? now;
  final SyncScheduler? scheduler;
  final Notifier? notifier;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) => controller.loaded
          ? HomeScreen(controller: controller, now: now, scheduler: scheduler, notifier: notifier)
          : const Scaffold(body: Center(child: CircularProgressIndicator())),
    );
  }
}
