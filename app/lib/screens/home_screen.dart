import 'package:flutter/material.dart';

import '../api.dart';
import '../background.dart';
import '../controller.dart';
import '../format.dart';
import '../models.dart';
import '../notifications.dart';
import '../theme.dart';
import '../timetrack.dart';
import '../theme_mode.dart';
import '../widgets/next_card.dart';
import 'more_screen.dart';
import 'time_screen.dart';
import '../widgets/loadable.dart';
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
  const HomeScreen({super.key, required this.controller, this.themeMode, this.now, this.scheduler, this.notifier});

  final AppController controller;
  final ThemeModeController? themeMode;
  final DateTime Function()? now;

  /// Replaceable in tests; the phone uses WorkManager and system notifications.
  final SyncScheduler? scheduler;
  final Notifier? notifier;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> with WidgetsBindingObserver {
  final _week = Loadable<WeekData>();
  final _history = Loadable<List<RiskDay>>();
  DateTime _loadedAt = DateTime.now();
  bool _checkinDone = true; // hidden until the stored date is read
  int _tab = 0;
  final Set<int> _opened = {0};
  final ValueNotifier<int> _calendarTick = ValueNotifier(0);

  AppController get _c => widget.controller;
  Wall get _today => wallNow(widget.now?.call());

  /// After this long in the background the screens ask the server again.
  static const _resumeAfter = Duration(minutes: 2);

  @override
  void initState() {
    super.initState();
    _c.addListener(_onConnection);
    WidgetsBinding.instance.addObserver(this);
    _load();
    _readCheckinDate();
  }

  @override
  void dispose() {
    _c.removeListener(_onConnection);
    WidgetsBinding.instance.removeObserver(this);
    _calendarTick.dispose();
    _week.dispose();
    _history.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) return;
    _readCheckinDate(); // a new day may have begun
    if (DateTime.now().difference(_loadedAt) >= _resumeAfter) _reload();
  }

  /// Another server: what is shown belongs to the old one.
  void _onConnection() {
    _calendarTick.value++;
    setState(() {});
    _load(clear: true);
  }

  /// The week and the colours of the last days. What was shown stays until
  /// the answers come; on the first start, the answers kept on the phone.
  Future<void> _load({bool clear = false}) {
    _loadedAt = DateTime.now();
    final api = _c.api;
    if (api == null) return Future.value();
    final kept = api is CachedReads ? api as CachedReads : null;
    return Future.wait([
      _week.load(api.week(), kept: kept?.keptWeek(), clear: clear),
      if (api is TimeApi) _history.load((api as TimeApi).riskHistory(), kept: kept?.keptRiskHistory(), clear: clear),
    ]);
  }

  Future<void> _readCheckinDate() async {
    final last = await _c.storage.lastCheckinDate();
    if (mounted) setState(() => _checkinDone = last == _today.ymd);
  }

  /// Asks again after a change: on this screen and on the tabs that listen.
  Future<void> _reload() {
    _calendarTick.value++;
    return _load();
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
    if (api == null) return _todayTab();
    // The tabs stay alive once opened: switching back does not reload them.
    // The key rebuilds them when the server settings change.
    final id = identityHashCode(api);
    Widget tab(int i) {
      if (i != _tab && !_opened.contains(i)) return const SizedBox.shrink();
      final key = ValueKey('${i}_$id');
      return switch (i) {
        0 => _todayTab(),
        1 => WeekScreen(key: key, api: api, now: widget.now, reload: _calendarTick),
        2 => TimeScreen(key: key, api: api, now: widget.now, reload: _calendarTick),
        3 => ReviewScreen(key: key, api: api),
        _ => MoreScreen(
            controller: _c,
            themeMode: widget.themeMode ?? ThemeModeController(),
            scheduler: widget.scheduler,
            notifier: widget.notifier,
          ),
      };
    }

    return IndexedStack(index: _tab, children: [for (var i = 0; i < 5; i++) tab(i)]);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(bottom: false, child: _tabBody()),
      bottomNavigationBar: _c.api == null
          ? null
          : NavigationBar(
              selectedIndex: _tab,
              onDestinationSelected: (i) => setState(() {
                _tab = i;
                _opened.add(i);
              }),
              destinations: const [
                NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Сегодня'),
                NavigationDestination(icon: Icon(Icons.calendar_month_outlined), selectedIcon: Icon(Icons.calendar_month), label: 'Неделя'),
                NavigationDestination(icon: Icon(Icons.schedule), selectedIcon: Icon(Icons.access_time_filled), label: 'Время'),
                NavigationDestination(icon: Icon(Icons.track_changes), label: 'Цели'),
                NavigationDestination(icon: Icon(Icons.menu), label: 'Ещё'),
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
              if (_c.api == null) IconButton(tooltip: 'Настройки', icon: const Icon(Icons.tune), onPressed: _openSettings),
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
    if (_c.api == null) {
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
    return LoadableView<WeekData>(
      source: _week,
      onRetry: _reload,
      onSettings: _openSettings,
      builder: (context, week) {
        return RefreshIndicator(
          onRefresh: _reload,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
            children: [
              NextCard(week: week, now: _today),
              if (!_checkinDone) _CheckinCard(onTap: _openCheckin),
              if (week.warnings.isNotEmpty) _Warnings(week.warnings),
              ListenableBuilder(
                listenable: _history,
                builder: (context, _) {
                  final days = _history.data;
                  RiskColor c(String n) => RiskColor.values.firstWhere((x) => x.name == n, orElse: () => RiskColor.gray);
                  final spark = days == null || days.isEmpty
                      ? null
                      : {
                          'Недосып': [for (final d in days) c(d.sleep)],
                          'Перегруз': [for (final d in days) c(d.overload)],
                          'Нет отдыха': [for (final d in days) c(d.noRest)],
                          'Состояние': [for (final d in days) c(d.state)],
                        };
                  return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    if (days != null && days.isNotEmpty) _WeekStrip(week: week, days: days, today: _today),
                    if (spark != null)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 8),
                        child: Row(
                          mainAxisAlignment: MainAxisAlignment.spaceBetween,
                          crossAxisAlignment: CrossAxisAlignment.baseline,
                          textBaseline: TextBaseline.alphabetic,
                          children: [
                            Text('Риски', style: Theme.of(context).textTheme.titleLarge),
                            Text('динамика за 7 дней', style: Theme.of(context).textTheme.bodySmall),
                          ],
                        ),
                      ),
                    RiskGrid(risks: week.risks, history: spark),
                  ]);
                },
              ),
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

/// The days of this week with the state of each: the worst risk of that day.
class _WeekStrip extends StatelessWidget {
  const _WeekStrip({required this.week, required this.days, required this.today});

  final WeekData week;
  final List<RiskDay> days;
  final Wall today;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    final byDate = {for (final d in days) d.date: d};
    final monday = week.from;
    RiskColor c(String n) => RiskColor.values.firstWhere((x) => x.name == n, orElse: () => RiskColor.gray);
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          crossAxisAlignment: CrossAxisAlignment.baseline,
          textBaseline: TextBaseline.alphabetic,
          children: [
            Text('Эта неделя', style: theme.textTheme.titleLarge),
            Text(weekRange(week.from, week.to), style: theme.textTheme.bodySmall),
          ],
        ),
        const SizedBox(height: 8),
        Row(children: [
          for (var i = 0; i < 7; i++)
            Expanded(
              child: Builder(builder: (context) {
                final d = monday.addDays(i);
                final r = byDate[d.ymd];
                final isToday = d.sameDay(today);
                final color = r == null ? null : riskColor(c(r.worst), b);
                return Semantics(
                  label: '${weekdayLong[i]}, ${d.day}: ${r == null ? 'ещё не было' : riskLabel(c(r.worst))}',
                  excludeSemantics: true,
                  child: Container(
                    margin: const EdgeInsets.symmetric(horizontal: 2),
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    decoration: BoxDecoration(
                      color: isToday ? pal.container : pal.surface,
                      borderRadius: BorderRadius.circular(16),
                      border: Border.all(color: isToday ? pal.primary : pal.line, width: isToday ? 2 : 1),
                    ),
                    child: Column(children: [
                      Text(weekdayShort[i][0].toUpperCase() + weekdayShort[i].substring(1), style: theme.textTheme.bodySmall),
                      Text('${d.day}', style: theme.textTheme.titleSmall),
                      const SizedBox(height: 4),
                      Container(
                        width: 22,
                        height: 22,
                        decoration: BoxDecoration(color: color ?? Colors.transparent, shape: BoxShape.circle, border: color == null ? Border.all(color: pal.line) : null),
                        child: color == null
                            ? null
                            : Icon(riskIcon(c(r!.worst)), size: 14, color: ThemeData.estimateBrightnessForColor(color) == Brightness.dark ? Colors.white : const Color(0xFF1A1A1A)),
                      ),
                    ]),
                  ),
                );
              }),
            ),
        ]),
      ]),
    );
  }
}
