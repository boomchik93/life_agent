import 'package:flutter/material.dart';

import '../background.dart';
import '../controller.dart';
import '../format.dart';
import '../models.dart';
import '../notifications.dart';
import '../theme.dart';
import '../widgets/error_view.dart';
import '../widgets/composer.dart';
import '../widgets/risk_indicator.dart';
import '../widgets/week_view.dart';
import 'advice_screen.dart';
import 'checkin_screen.dart';
import 'models_screen.dart';
import 'plan_screen.dart';
import 'review_screen.dart';
import 'settings_screen.dart';
import 'week_screen.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.controller, this.now, this.scheduler, this.notifier});

  final AppController controller;
  final DateTime Function()? now;

  /// Replaceable in tests; the phone uses WorkManager and system notifications.
  final SyncScheduler? scheduler;
  final Notifier? notifier;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Future<WeekData>? _future;
  bool _checkinDone = true; // hidden until the stored date is read
  int _tab = 0;

  AppController get _c => widget.controller;
  Wall get _today => wallNow(widget.now?.call());

  @override
  void initState() {
    super.initState();
    _c.addListener(_reload);
    _future = _c.api?.week();
    _readCheckinDate();
  }

  @override
  void dispose() {
    _c.removeListener(_reload);
    super.dispose();
  }

  Future<void> _readCheckinDate() async {
    final last = await _c.storage.lastCheckinDate();
    if (mounted) setState(() => _checkinDone = last == _today.ymd);
  }

  void _reload() {
    final future = _c.api?.week();
    setState(() {
      _future = future;
    });
  }

  Future<void> _openCheckin() async {
    final api = _c.api;
    if (api == null) return;
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => CheckinScreen(api: api)),
    );
    if (saved == true) {
      await _c.storage.saveCheckinDate(_today.ymd);
      if (!mounted) return;
      setState(() => _checkinDone = true);
      _reload();
    }
  }

  Future<void> _openSettings() async {
    await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => SettingsScreen(controller: _c)),
    );
    // The controller notifies on save, which reloads the week.
  }

  void _openModels() {
    final api = _c.api;
    if (api == null) return;
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => ModelsScreen(api: api)));
  }

  Future<void> _openPlan() async {
    final api = _c.api;
    if (api == null) return;
    final written = await Navigator.of(context).push<bool>(MaterialPageRoute(builder: (_) => PlanScreen(api: api)));
    if (written == true) _reload();
  }

  void _openAdvice({bool askNow = false}) {
    final api = _c.api;
    if (api == null) return;
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => AdviceScreen(
        api: api,
        storage: _c.storage,
        scheduler: widget.scheduler ?? WorkmanagerScheduler(),
        notifier: widget.notifier ?? LocalNotifier(),
        askNow: askNow,
      ),
    ));
  }

  String _greeting() {
    final h = (widget.now?.call() ?? DateTime.now()).hour;
    if (h < 5) return 'Доброй ночи';
    if (h < 12) return 'Доброе утро';
    if (h < 18) return 'Добрый день';
    return 'Добрый вечер';
  }

  Widget _tabBody() {
    final api = _c.api;
    if (_tab == 0 || api == null) return _todayTab();
    // Each tab is a full screen with its own app bar; the key rebuilds it
    // when the server settings change.
    final key = ValueKey('${_tab}_${identityHashCode(api)}');
    return switch (_tab) {
      1 => WeekScreen(key: key, api: api, now: widget.now),
      2 => ReviewScreen(key: key, api: api),
      _ => AdviceScreen(
          key: key,
          api: api,
          storage: _c.storage,
          scheduler: widget.scheduler ?? WorkmanagerScheduler(),
          notifier: widget.notifier ?? LocalNotifier(),
        ),
    };
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(bottom: false, child: _tabBody()),
      bottomNavigationBar: _c.api == null
          ? null
          : NavigationBar(
              selectedIndex: _tab,
              onDestinationSelected: (i) => setState(() => _tab = i),
              destinations: const [
                NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Сегодня'),
                NavigationDestination(icon: Icon(Icons.calendar_month_outlined), selectedIcon: Icon(Icons.calendar_month), label: 'Неделя'),
                NavigationDestination(icon: Icon(Icons.track_changes), label: 'Цели'),
                NavigationDestination(icon: Icon(Icons.lightbulb_outline), selectedIcon: Icon(Icons.lightbulb), label: 'Совет'),
              ],
            ),
    );
  }

  Widget _todayTab() {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final t = _today;
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 20, 8, 0),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('${weekdayLong[t.weekday - 1]}, ${shortDate(t)}',
                        style: theme.textTheme.bodyMedium?.copyWith(color: pal.text2, fontWeight: FontWeight.w600)),
                    Text(_greeting(), style: theme.textTheme.headlineLarge),
                  ],
                ),
              ),
              if (_c.api != null)
                IconButton(tooltip: 'План недели', icon: const Icon(Icons.auto_awesome), onPressed: _openPlan),
              IconButton(tooltip: 'Настройки', icon: const Icon(Icons.tune), onPressed: _openSettings),
            ],
          ),
        ),
        Expanded(child: _body()),
        if (_c.api != null)
          Composer(
            api: _c.api!,
            now: widget.now,
            onOpenModels: _openModels,
            onSaved: _reload,
            onAdvice: () => _openAdvice(askNow: true),
          ),
      ],
    );
  }

  Widget _body() {
    final future = _future;
    if (future == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('Укажи адрес сервера и токен.', textAlign: TextAlign.center),
              const SizedBox(height: 16),
              FilledButton(onPressed: _openSettings, child: const Text('Настройки')),
            ],
          ),
        ),
      );
    }
    return FutureBuilder<WeekData>(
      future: future,
      builder: (context, snap) {
        if (snap.connectionState != ConnectionState.done) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snap.hasError) {
          return ErrorView(error: snap.error!, onRetry: _reload, onSettings: _openSettings);
        }
        final week = snap.data!;
        return RefreshIndicator(
          onRefresh: () async {
            _reload();
            await _future!.catchError((_) => week);
          },
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
            children: [
              if (!_checkinDone) _CheckinCard(onTap: _openCheckin),
              if (week.warnings.isNotEmpty) _Warnings(week.warnings),
              RiskGrid(risks: week.risks),
              Padding(
                padding: const EdgeInsets.only(top: 20),
                child: Wrap(
                  alignment: WrapAlignment.spaceBetween,
                  crossAxisAlignment: WrapCrossAlignment.end,
                  spacing: 12,
                  children: [
                    Text('Неделя', style: Theme.of(context).textTheme.titleLarge),
                    Text(weekRange(week.from, week.to), style: Theme.of(context).textTheme.bodySmall),
                  ],
                ),
              ),
              WeekView(week: week, today: _today),
            ],
          ),
        );
      },
    );
  }
}

class _CheckinCard extends StatelessWidget {
  const _CheckinCard({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Material(
        color: pal.container,
        borderRadius: BorderRadius.circular(24),
        child: InkWell(
          borderRadius: BorderRadius.circular(24),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(color: pal.primary, shape: BoxShape.circle),
                child: Icon(Icons.favorite_border, color: pal.onPrimary),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text('Как ты сегодня?', style: theme.textTheme.titleLarge?.copyWith(color: pal.onContainer)),
                  Text('Чек-ин займёт 5 секунд', style: theme.textTheme.bodyMedium?.copyWith(color: pal.onContainer)),
                ]),
              ),
              Icon(Icons.chevron_right, color: pal.onContainer),
            ]),
          ),
        ),
      ),
    );
  }
}

class _Warnings extends StatelessWidget {
  const _Warnings(this.warnings);

  final List<String> warnings;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: Theme.of(context).colorScheme.errorContainer,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Icon(Icons.sync_problem),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                'Часть календарей не прочиталась, оценка неполная:\n${warnings.join('\n')}',
              ),
            ),
          ],
        ),
      ),
    );
  }
}
