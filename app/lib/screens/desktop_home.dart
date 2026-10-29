import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../models.dart';
import '../theme.dart';
import '../theme_mode.dart';
import '../timetrack.dart';
import '../widgets/composer.dart';
import '../widgets/decor.dart';
import '../widgets/loadable.dart';
import '../widgets/motion.dart';
import '../widgets/next_card.dart';
import '../widgets/risk_indicator.dart';

/// A window this wide gets the side menu and the home screen in two columns.
const desktopFrom = 1000.0;

/// The sections of the side menu, in its order.
const desktopSections = [
  (Icons.home_outlined, Icons.home, 'Сегодня'),
  (Icons.calendar_month_outlined, Icons.calendar_month, 'Неделя'),
  (Icons.schedule, Icons.access_time_filled, 'Время'),
  (Icons.track_changes, Icons.track_changes, 'Цели'),
  (Icons.lightbulb_outline, Icons.lightbulb, 'Совет'),
];

/// The index of the settings page, after the sections.
const desktopSettings = 5;

/// The side menu of a wide window: the sections, then the models, the theme
/// and the settings at the bottom.
class DesktopSidebar extends StatelessWidget {
  const DesktopSidebar({super.key, required this.selected, required this.onSelect, required this.themeMode, this.onModels});

  final int selected;
  final ValueChanged<int> onSelect;
  final ThemeModeController themeMode;
  final VoidCallback? onModels;

  static const _next = {ThemeMode.system: ThemeMode.light, ThemeMode.light: ThemeMode.dark, ThemeMode.dark: ThemeMode.system};

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return Container(
      width: 248,
      decoration: BoxDecoration(color: pal.side, border: Border(right: BorderSide(color: pal.line))),
      padding: const EdgeInsets.fromLTRB(14, 20, 14, 20),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(8, 4, 8, 18),
          child: Row(children: [
            Container(
              width: 36,
              height: 36,
              decoration: heroDecoration(pal, radius: 12),
              child: Icon(Icons.track_changes, size: 20, color: pal.onHero),
            ),
            const SizedBox(width: 10),
            Text('Жизнь', style: TextStyle(fontFamily: fontNumbers, fontWeight: FontWeight.w600, fontSize: 22, color: pal.text)),
          ]),
        ),
        for (var i = 0; i < desktopSections.length; i++)
          _Item(
            icon: selected == i ? desktopSections[i].$2 : desktopSections[i].$1,
            label: desktopSections[i].$3,
            on: selected == i,
            onTap: () => onSelect(i),
          ),
        const Spacer(),
        if (onModels != null) _Item(icon: Icons.memory, label: 'Модели', onTap: onModels!),
        ValueListenableBuilder<ThemeMode>(
          valueListenable: themeMode,
          builder: (context, mode, _) => _Item(
            icon: Icons.light_mode_outlined,
            label: 'Тема: ${themeModeName(mode).toLowerCase()}',
            onTap: () => themeMode.choose(_next[mode]!),
          ),
        ),
        _Item(
          icon: selected == desktopSettings ? Icons.settings : Icons.settings_outlined,
          label: 'Настройки',
          on: selected == desktopSettings,
          onTap: () => onSelect(desktopSettings),
        ),
      ]),
    );
  }
}

class _Item extends StatelessWidget {
  const _Item({required this.icon, required this.label, required this.onTap, this.on = false});

  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final bool on;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final color = on ? pal.onContainer : pal.text2;
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Semantics(
        selected: on,
        button: true,
        child: Material(
          color: on ? pal.container : Colors.transparent,
          borderRadius: BorderRadius.circular(24),
          child: InkWell(
            borderRadius: BorderRadius.circular(24),
            onTap: onTap,
            child: ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 48),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 14),
                child: Row(children: [
                  Icon(icon, color: color),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(label,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontWeight: FontWeight.w600, fontSize: 16, color: color)),
                  ),
                ]),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// The home screen of a wide window: how the day feels and the risks with
/// the phrase line on the left; the load of the week and today's events on
/// the right. The arrows move the week; the rest is about today.
class DesktopToday extends StatefulWidget {
  const DesktopToday({
    super.key,
    required this.api,
    required this.week,
    required this.history,
    required this.greeting,
    required this.today,
    required this.checkinDone,
    required this.onCheckin,
    required this.onPlan,
    required this.onReload,
    this.onSettings,
    this.onModels,
    this.onAdvice,
    this.now,
  });

  final LifeApi api;
  final Loadable<WeekData> week;
  final Loadable<List<RiskDay>> history;
  final String greeting;
  final Wall today;
  final bool checkinDone;

  /// Opens the check-in with the mood picked here, 1 to 5.
  final ValueChanged<int> onCheckin;
  final VoidCallback onPlan;
  final Future<void> Function() onReload;
  final VoidCallback? onSettings;
  final VoidCallback? onModels;
  final VoidCallback? onAdvice;
  final DateTime Function()? now;

  @override
  State<DesktopToday> createState() => _DesktopTodayState();
}

class _DesktopTodayState extends State<DesktopToday> {
  int _shift = 0; // weeks from this one
  final _other = Loadable<WeekData>(); // the week the arrows moved to

  @override
  void dispose() {
    _other.dispose();
    super.dispose();
  }

  void _move(int weeks) {
    final shift = weeks == 0 ? 0 : _shift + weeks;
    setState(() => _shift = shift);
    if (shift == 0) return;
    final current = widget.week.data;
    if (current == null) return;
    final start = current.from.addDays(7 * shift);
    final api = widget.api;
    final kept = api is CachedReads ? (api as CachedReads).keptWeek(start: start) : null;
    _other.load(api.week(start: start), kept: kept, clear: true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(36, 28, 36, 28),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(children: [
          GreetingBadge(hour: (widget.now?.call() ?? DateTime.now()).hour, size: 56),
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(widget.greeting, style: theme.textTheme.headlineLarge?.copyWith(fontSize: 30, fontWeight: FontWeight.w800)),
              const SizedBox(height: 4),
              Text(dayTitle(widget.today), style: theme.textTheme.bodyMedium?.copyWith(color: pal.text3)),
            ]),
          ),
          IconButton.filledTonal(
            tooltip: 'Прошлая неделя',
            style: IconButton.styleFrom(minimumSize: const Size(48, 48), backgroundColor: pal.surface2, foregroundColor: pal.text),
            onPressed: () => _move(-1),
            icon: const Icon(Icons.chevron_left),
          ),
          const SizedBox(width: 8),
          FilledButton.tonal(
            style: FilledButton.styleFrom(minimumSize: const Size(0, 48)),
            onPressed: _shift == 0 ? widget.onReload : () => _move(0),
            child: const Text('Сегодня'),
          ),
          const SizedBox(width: 8),
          IconButton.filledTonal(
            tooltip: 'Следующая неделя',
            style: IconButton.styleFrom(minimumSize: const Size(48, 48), backgroundColor: pal.surface2, foregroundColor: pal.text),
            onPressed: () => _move(1),
            icon: const Icon(Icons.chevron_right),
          ),
        ]),
        const SizedBox(height: 20),
        Expanded(
          child: LoadableView<WeekData>(
            source: widget.week,
            onRetry: widget.onReload,
            onSettings: widget.onSettings,
            builder: (context, week) => Row(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Expanded(child: _left(context, week)),
              const SizedBox(width: 28),
              Expanded(child: _right(context, week)),
            ]),
          ),
        ),
      ]),
    );
  }

  Widget _left(BuildContext context, WeekData week) {
    return CustomScrollView(slivers: [
      SliverFillRemaining(
        hasScrollBody: false,
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Appear(child: NextCard(week: week, now: widget.today)),
          const SizedBox(height: 2),
          Appear(index: 1, child: _CheckinCard(done: widget.checkinDone, onPick: widget.onCheckin)),
          const SizedBox(height: 16),
          if (week.warnings.isNotEmpty) ...[
            _DeskCard(child: Text(week.warnings.join('\n'), style: Theme.of(context).textTheme.bodyMedium)),
            const SizedBox(height: 16),
          ],
          Appear(
            index: 2,
            child: ListenableBuilder(
              listenable: widget.history,
              builder: (context, _) => RiskGrid(risks: week.risks, history: riskSpark(widget.history.data)),
            ),
          ),
          const Spacer(),
          const SizedBox(height: 16),
          Composer(
            api: widget.api,
            now: widget.now,
            inline: true,
            onOpenModels: widget.onModels,
            onSaved: widget.onReload,
            onAdvice: widget.onAdvice,
          ),
        ]),
      ),
    ]);
  }

  Widget _right(BuildContext context, WeekData current) {
    final weekCard = _shift == 0
        ? _WeekLoad(week: current, today: widget.today, onPlan: widget.onPlan)
        : LoadableView<WeekData>(
            source: _other,
            expand: false,
            placeholder: const _DeskCard(child: SizedBox(height: 120, child: Center(child: CircularProgressIndicator()))),
            onRetry: () => _move(0),
            builder: (context, w) => _WeekLoad(week: w, today: widget.today, onPlan: widget.onPlan),
          );
    return CustomScrollView(slivers: [
      SliverFillRemaining(
        hasScrollBody: false,
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Appear(index: 1, child: weekCard),
          const SizedBox(height: 16),
          Expanded(child: Appear(index: 2, child: _TodayEvents(week: current, today: widget.today))),
        ]),
      ),
    ]);
  }
}

/// The sparklines of the risk tiles from the colours of the last days.
Map<String, List<RiskColor>>? riskSpark(List<RiskDay>? days) {
  if (days == null || days.isEmpty) return null;
  RiskColor c(String n) => RiskColor.values.firstWhere((x) => x.name == n, orElse: () => RiskColor.gray);
  return {
    'Недосып': [for (final d in days) c(d.sleep)],
    'Перегруз': [for (final d in days) c(d.overload)],
    'Нет отдыха': [for (final d in days) c(d.noRest)],
    'Состояние': [for (final d in days) c(d.state)],
  };
}

class _DeskCard extends StatelessWidget {
  const _DeskCard({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return Container(padding: const EdgeInsets.all(18), decoration: cardDecoration(pal), child: child);
  }
}

const _faces = [
  (Icons.sentiment_very_dissatisfied, 'Плохо'),
  (Icons.sentiment_dissatisfied, 'Так себе'),
  (Icons.sentiment_neutral, 'Нормально'),
  (Icons.sentiment_satisfied, 'Хорошо'),
  (Icons.sentiment_very_satisfied, 'Отлично'),
];

/// The colour of a face, from the bad mood to the good one.
Color _faceColor(int i, Palette pal) {
  final b = pal.brightness;
  return switch (i) {
    0 => riskColor(RiskColor.red, b),
    1 => pal.accent,
    2 => riskColor(RiskColor.yellow, b),
    3 => Color.lerp(riskColor(RiskColor.yellow, b), riskColor(RiskColor.green, b), 0.6)!,
    _ => riskColor(RiskColor.green, b),
  };
}

/// "Как ты сегодня?": a face starts the check-in with that mood.
class _CheckinCard extends StatelessWidget {
  const _CheckinCard({required this.done, required this.onPick});

  final bool done;
  final ValueChanged<int> onPick;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return _DeskCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Как ты сегодня?', style: theme.textTheme.titleLarge),
        if (done)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text('Сегодня уже отмечено. Можно изменить.', style: theme.textTheme.bodySmall?.copyWith(color: pal.text3)),
          ),
        const SizedBox(height: 12),
        Row(children: [
          for (var i = 0; i < _faces.length; i++) ...[
            if (i > 0) const SizedBox(width: 10),
            Expanded(
              child: Semantics(
                button: true,
                label: 'Настроение: ${_faces[i].$2}',
                child: Material(
                  color: Color.alphaBlend(_faceColor(i, pal).withValues(alpha: 0.10), pal.surface),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(20),
                    side: BorderSide(color: _faceColor(i, pal).withValues(alpha: 0.35), width: 2),
                  ),
                  child: InkWell(
                    borderRadius: BorderRadius.circular(20),
                    onTap: () => onPick(i + 1),
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(minHeight: 80),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 8, horizontal: 4),
                        child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                          Icon(_faces[i].$1, size: 32, color: _faceColor(i, pal)),
                          const SizedBox(height: 6),
                          Text(_faces[i].$2,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: pal.text2)),
                        ]),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ]),
      ]),
    );
  }
}

/// How full a day is by the busy minutes of its timed events: the share of
/// the bar, the word and the colour. Nine busy hours fill the bar.
(double, String, RiskColor) dayLoad(int minutes) {
  final h = minutes / 60;
  final share = (h / 9).clamp(0.0, 1.0);
  if (h < 1.5) return (share, 'Свободно', RiskColor.green);
  if (h < 4) return (share, 'Спокойно', RiskColor.green);
  if (h < 6) return (share, 'Нормально', RiskColor.green);
  if (h < 8) return (share, 'Много', RiskColor.yellow);
  return (share, 'Перегруз', RiskColor.red);
}

/// The busy minutes of [day]: the timed events, cut at midnight.
int busyMinutes(WeekData week, Wall day) {
  final from = day.dayStart, to = day.dayStart.addDays(1);
  var sum = 0;
  for (final e in week.events) {
    if (e.allDay) continue;
    final s = e.start.isBefore(from) ? from : e.start;
    final en = to.isBefore(e.end) ? to : e.end;
    if (s.isBefore(en)) sum += en.minutesSince(s);
  }
  return sum;
}

/// "Неделя": a bar per day, today marked.
class _WeekLoad extends StatelessWidget {
  const _WeekLoad({required this.week, required this.today, required this.onPlan});

  final WeekData week;
  final Wall today;
  final VoidCallback onPlan;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    return _DeskCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(children: [
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Неделя', style: theme.textTheme.titleLarge),
              Text(weekRange(week.from, week.to), style: theme.textTheme.bodySmall?.copyWith(color: pal.text3)),
            ]),
          ),
          FilledButton.tonal(
            style: FilledButton.styleFrom(minimumSize: const Size(0, 48)),
            onPressed: onPlan,
            child: const Text('Разложить задачи'),
          ),
        ]),
        const SizedBox(height: 8),
        for (var i = 0; i < 7; i++)
          Builder(builder: (context) {
            final day = week.from.addDays(i);
            final (share, word, color) = dayLoad(busyMinutes(week, day));
            final isToday = day.sameDay(today);
            return Container(
              constraints: const BoxConstraints(minHeight: 52),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
              decoration: BoxDecoration(color: isToday ? pal.container : null, borderRadius: BorderRadius.circular(18)),
              child: Row(children: [
                SizedBox(
                  width: 72,
                  child: Text(
                    '${weekdayShort[i][0].toUpperCase()}${weekdayShort[i].substring(1)}',
                    style: theme.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
                  ),
                ),
                Expanded(
                  child: Semantics(
                    label: '${dayTitle(day)}: $word',
                    excludeSemantics: true,
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: SizedBox(
                        height: 14,
                        child: Stack(children: [
                          Positioned.fill(child: ColoredBox(color: pal.surface2)),
                          Grow(
                            value: share,
                            ms: 700 + i * 60,
                            builder: (context, v) => FractionallySizedBox(
                              widthFactor: v,
                              child: DecoratedBox(
                                decoration: BoxDecoration(
                                  borderRadius: BorderRadius.circular(7),
                                  gradient: LinearGradient(colors: [riskColor(color, b).withValues(alpha: 0.65), riskColor(color, b)]),
                                ),
                                child: const SizedBox.expand(),
                              ),
                            ),
                          ),
                        ]),
                      ),
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                SizedBox(
                  width: 84,
                  child: Text(word, textAlign: TextAlign.right, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: pal.text2)),
                ),
              ]),
            );
          }),
      ]),
    );
  }
}

const _categoryNames = {
  'study': 'Учёба',
  'work': 'Работа',
  'sport': 'Спорт',
  'hobby': 'Хобби',
  'rest': 'Отдых',
  'chores': 'Дела',
};

/// "Сегодня": the events of the day with their category.
class _TodayEvents extends StatelessWidget {
  const _TodayEvents({required this.week, required this.today});

  final WeekData week;
  final Wall today;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    final from = today.dayStart, to = from.addDays(1);
    final events = [
      for (final e in week.events)
        if (e.start.isBefore(to) && from.isBefore(e.end)) e,
    ]..sort((a, c) => a.start.minutesSince(c.start));
    return _DeskCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text('Сегодня', style: theme.textTheme.titleLarge),
        const SizedBox(height: 6),
        if (events.isEmpty)
          const EmptyState(icon: Icons.wb_sunny_outlined, title: 'Событий нет', text: 'День свободен.', compact: true),
        for (var i = 0; i < events.length; i++)
          Container(
            constraints: const BoxConstraints(minHeight: 56),
            margin: const EdgeInsets.only(top: 6),
            padding: const EdgeInsets.fromLTRB(10, 6, 12, 6),
            decoration: BoxDecoration(
              color: Color.alphaBlend(categoryColor(events[i].category, b).withValues(alpha: 0.10), pal.surface),
              borderRadius: BorderRadius.circular(16),
            ),
            child: Row(children: [
              Container(
                width: 12,
                height: 36,
                decoration: BoxDecoration(color: categoryColor(events[i].category, b), borderRadius: BorderRadius.circular(6)),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(events[i].title, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700)),
                  Text(eventTime(events[i], today), style: theme.textTheme.bodySmall?.copyWith(color: pal.text3)),
                ]),
              ),
              if (_categoryNames[events[i].category] != null)
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                  decoration: BoxDecoration(
                    color: categoryColor(events[i].category, b).withValues(alpha: 0.16),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(_categoryNames[events[i].category]!,
                      style: TextStyle(fontSize: 13, fontWeight: FontWeight.w700, color: categoryColor(events[i].category, b))),
                ),
            ]),
          ),
      ]),
    );
  }
}
