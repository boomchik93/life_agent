import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'background.dart';
import 'controller.dart';
import 'notifications.dart';
import 'platform.dart';
import 'screens/home_screen.dart';
import 'settings.dart';
import 'theme.dart';
import 'theme_mode.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final storage = PlatformStorage();
  final themeMode = ThemeModeController();
  await themeMode.load();
  runApp(LifeAgentApp(controller: AppController(storage)..load(), themeMode: themeMode));
  // After the first frame is on its way: the screens do not wait for it.
  if (isPhone) unawaited(_scheduleBackground(storage));
}

Future<void> _scheduleBackground(AppStorage storage) async {
  try {
    await WorkmanagerScheduler.init();
    // The system keeps the schedule, but a reinstall or an update may drop it.
    await applyBackground(storage, WorkmanagerScheduler());
  } catch (_) {
    // Background work is optional: the app works without it.
  }
}

class LifeAgentApp extends StatelessWidget {
  const LifeAgentApp({super.key, required this.controller, this.themeMode});

  final AppController controller;
  final ThemeModeController? themeMode;

  @override
  Widget build(BuildContext context) {
    final mode = themeMode ?? ThemeModeController();
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: mode,
      builder: (context, themeModeValue, _) => _app(themeModeValue, mode),
    );
  }

  Widget _app(ThemeMode themeModeValue, ThemeModeController mode) {
    return MaterialApp(
      title: 'Жизнь',
      themeMode: themeModeValue,
      debugShowCheckedModeBanner: false,
      locale: const Locale('ru'),
      supportedLocales: const [Locale('ru')],
      localizationsDelegates: GlobalMaterialLocalizations.delegates,
      theme: buildTheme(Palette.light),
      darkTheme: buildTheme(Palette.dark),
      home: HomeRoot(controller: controller, themeMode: mode),
    );
  }
}

/// Waits for the stored settings, then shows the home screen.
class HomeRoot extends StatelessWidget {
  const HomeRoot({super.key, required this.controller, this.themeMode, this.now, this.scheduler, this.notifier});

  final AppController controller;
  final ThemeModeController? themeMode;
  final DateTime Function()? now;
  final SyncScheduler? scheduler;
  final Notifier? notifier;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: controller,
      builder: (context, _) => controller.loaded
          ? HomeScreen(controller: controller, themeMode: themeMode, now: now, scheduler: scheduler, notifier: notifier)
          : const Scaffold(body: Center(child: CircularProgressIndicator())),
    );
  }
}
