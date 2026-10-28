import 'package:flutter/material.dart';

import '../api.dart';
import '../background.dart';
import '../controller.dart';
import '../health.dart';
import '../notifications.dart';
import '../platform.dart';
import '../theme.dart';
import '../theme_mode.dart';
import '../usage.dart';
import 'advice_screen.dart';
import 'health_screen.dart';
import 'models_screen.dart';
import 'settings_screen.dart';
import 'sources_screen.dart';

/// The "Ещё" tab: appearance and the sections that are not on the tab bar.
class MoreScreen extends StatelessWidget {
  const MoreScreen({super.key, required this.controller, required this.themeMode, this.scheduler, this.notifier});

  final AppController controller;
  final ThemeModeController themeMode;
  final SyncScheduler? scheduler;
  final Notifier? notifier;

  void _push(BuildContext context, Widget Function(LifeApi api) page) {
    final api = controller.api;
    if (api == null) return;
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => page(api)));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final hasApi = controller.api != null;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 20, 16, 24),
      children: [
        Text('Ещё', style: theme.textTheme.headlineLarge),
        _Section('Оформление'),
        _Group(children: [
          ValueListenableBuilder<ThemeMode>(
            valueListenable: themeMode,
            builder: (context, mode, _) => Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _Link(
                  icon: Icons.brightness_6_outlined,
                  title: 'Тема',
                  subtitle: mode == ThemeMode.system
                      ? 'Как в системе, сейчас ${theme.brightness == Brightness.dark ? 'тёмная' : 'светлая'}'
                      : themeModeName(mode),
                ),
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: SizedBox(
                    width: double.infinity,
                    child: SegmentedButton<ThemeMode>(
                      showSelectedIcon: false,
                      segments: [
                        for (final m in ThemeMode.values)
                          ButtonSegment(value: m, label: Text(themeModeName(m), maxLines: 1, overflow: TextOverflow.ellipsis)),
                      ],
                      selected: {mode},
                      onSelectionChanged: (s) => themeMode.choose(s.first),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ]),
        _Section('Разделы'),
        _Group(children: [
          _Link(
            icon: Icons.lightbulb_outline,
            title: 'Совет',
            subtitle: 'Как разгрузить неделю',
            onTap: !hasApi
                ? null
                : () => _push(
                      context,
                      (api) => AdviceScreen(
                        api: api,
                        storage: controller.storage,
                        scheduler: scheduler ?? WorkmanagerScheduler(),
                        notifier: notifier ?? LocalNotifier(),
                      ),
                    ),
          ),
          _Link(
            icon: Icons.settings_outlined,
            title: 'Настройки',
            subtitle: 'Подключение, данные',
            onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => SettingsScreen(controller: controller))),
          ),
          _Link(
            icon: Icons.storage_outlined,
            title: 'Источники данных',
            subtitle: hasApi ? 'Время в приложениях, Steam, GitHub, погода' : 'Сначала сохрани адрес и токен',
            onTap: !hasApi
                ? null
                : () => _push(
                      context,
                      (api) => SourcesScreen(
                        api: api,
                        storage: controller.storage,
                        usage: deviceUsageSource(),
                        scheduler: scheduler ?? WorkmanagerScheduler(),
                      ),
                    ),
          ),
          if (isPhone)
            _Link(
              icon: Icons.favorite_border,
              title: 'Здоровье',
              subtitle: hasApi ? 'Сон и пульс из Health Connect' : 'Сначала сохрани адрес и токен',
              onTap: !hasApi
                  ? null
                  : () => _push(
                        context,
                        (api) => HealthScreen(
                          api: api,
                          storage: controller.storage,
                          source: HealthConnectSource(),
                          scheduler: scheduler ?? WorkmanagerScheduler(),
                        ),
                      ),
            ),
          _Link(
            icon: Icons.memory,
            title: 'Модели ИИ',
            subtitle: hasApi ? 'Какая модель разбирает фразы' : 'Сначала сохрани адрес и токен',
            onTap: !hasApi ? null : () => _push(context, (api) => ModelsScreen(api: api)),
          ),
        ]),
        const SizedBox(height: 12),
        Text('Версия 1.0.0', style: theme.textTheme.bodySmall?.copyWith(color: pal.text3)),
      ],
    );
  }
}

class _Section extends StatelessWidget {
  const _Section(this.title);

  final String title;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.fromLTRB(4, 24, 4, 8),
        child: Text(title, style: Theme.of(context).textTheme.titleSmall?.copyWith(color: Palette.of(context).text2)),
      );
}

class _Group extends StatelessWidget {
  const _Group({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: pal.surface,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: pal.line),
      ),
      child: Column(children: children),
    );
  }
}

class _Link extends StatelessWidget {
  const _Link({required this.icon, required this.title, required this.subtitle, this.onTap});

  final IconData icon;
  final String title, subtitle;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return InkWell(
      borderRadius: BorderRadius.circular(12),
      onTap: onTap,
      child: Opacity(
        opacity: onTap == null && title != 'Тема' ? 0.5 : 1,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 10),
          child: Row(children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(color: pal.container, borderRadius: BorderRadius.circular(14)),
              child: Icon(icon, size: 22, color: pal.onContainer),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(title, style: theme.textTheme.titleSmall),
                Text(subtitle, style: theme.textTheme.bodySmall),
              ]),
            ),
            if (onTap != null) Icon(Icons.chevron_right, color: pal.text3),
          ]),
        ),
      ),
    );
  }
}
