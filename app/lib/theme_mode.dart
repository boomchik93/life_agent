import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// The chosen appearance: light, dark or the phone's own setting.
class ThemeModeController extends ValueNotifier<ThemeMode> {
  ThemeModeController() : super(ThemeMode.system);

  static const _key = 'theme_mode';

  Future<void> load() async {
    try {
      final name = (await SharedPreferences.getInstance()).getString(_key);
      value = ThemeMode.values.firstWhere((m) => m.name == name, orElse: () => ThemeMode.system);
    } catch (_) {
      // Storage is optional: the system theme stays.
    }
  }

  Future<void> choose(ThemeMode mode) async {
    value = mode;
    try {
      await (await SharedPreferences.getInstance()).setString(_key, mode.name);
    } catch (_) {}
  }
}

String themeModeName(ThemeMode m) => switch (m) {
      ThemeMode.light => 'Светлая',
      ThemeMode.dark => 'Тёмная',
      ThemeMode.system => 'Как в системе',
    };
