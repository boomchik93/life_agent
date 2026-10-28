import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../models.dart';
import '../theme.dart';
import '../widgets/loadable.dart';
import '../widgets/risk_indicator.dart';
import '../widgets/week_view.dart';
import 'plan_screen.dart';

/// Any week of the calendar: a grid of busy hours or a list, with arrows to
/// move between weeks.
class WeekScreen extends StatefulWidget {
  const WeekScreen({super.key, required this.api, this.now, this.reload});

  final LifeApi api;
  final DateTime Function()? now;

  /// Bumped by the home screen when the calendar changed (an event was written).
  final ValueListenable<int>? reload;

  @override
  State<WeekScreen> createState() => _WeekScreenState();
}

const _gridFrom = 8, _gridHours = 16; // 08:00 to 24:00

class _WeekScreenState extends State<WeekScreen> {
  Wall? _start; // null: current week
  final _week = Loadable<WeekData>();
  WeekData? _shown;
  bool _grid = true;
  Wall? _picked; // the day opened under the grid

  @override
  void initState() {
    super.initState();
    widget.reload?.addListener(_onReload);
    _load(null);
  }

  @override
  void dispose() {
    widget.reload?.removeListener(_onReload);
    _week.dispose();
    super.dispose();
  }

  void _onReload() => _load(_start);

  /// Shows the week of [start]. Another week replaces the shown one with its
  /// kept copy, or with a spinner; the same week stays while it is renewed.
  Future<void> _load(Wall? start) {
    final api = widget.api;
    final kept = api is CachedReads ? (api as CachedReads).keptWeek(start: start) : null;
    final other = start != _start;
    setState(() => _start = start);
    return _week.load(api.week(start: start), kept: kept, clear: other);
  }

  void _move(int weeks) {
    // From the week asked for last: a second tap does not wait for the first.
    final from = _start ?? _shown?.from;
    if (from == null) return;
    _picked = null;
    _load(from.addDays(7 * weeks));
  }

  Future<void> _openPlan() async {
    final written = await Navigator.of(context).push<bool>(MaterialPageRoute(builder: (_) => PlanScreen(api: widget.api)));
    if (written == true && mounted) _load(_start);
  }

  @override
  Widget build(BuildContext context) {
    final today = wallNow(widget.now?.call());
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return Column(
      children: [
        SizedBox(
          height: 64,
          child: Row(children: [
            const SizedBox(width: 8),
            IconButton(tooltip: 'Прошлая неделя', icon: const Icon(Icons.chevron_left), onPressed: () => _move(-1)),
            Expanded(
              child: ListenableBuilder(
                listenable: _week,
                builder: (_, _) {
                  final w = _week.data;
                  return Text(
                    w != null ? weekRange(w.from, w.to) : 'Неделя',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.titleLarge,
                  );
                },
              ),
            ),
            IconButton(tooltip: 'Следующая неделя', icon: const Icon(Icons.chevron_right), onPressed: () => _move(1)),
            const SizedBox(width: 8),
          ]),
        ),
        Expanded(
          child: LoadableView<WeekData>(
            source: _week,
            onRetry: () => _load(_start),
            builder: (context, week) {
              _shown = week;
              final days = [for (var i = 0; i < 7; i++) week.from.addDays(i)];
              final picked = _picked != null && days.any((d) => d.sameDay(_picked!))
                  ? _picked!
                  : (days.any((d) => d.sameDay(today)) ? today : days.first);
              return RefreshIndicator(
                onRefresh: () => _load(_start),
                child: ListView(
                  padding: const EdgeInsets.fromLTRB(16, 2, 16, 24),
                  children: [
                    Row(children: [
                      Expanded(
                        child: Container(
                          padding: const EdgeInsets.all(4),
                          decoration: BoxDecoration(color: pal.surface2, borderRadius: BorderRadius.circular(20)),
                          child: Row(children: [
                            _Seg(icon: Icons.grid_view, label: 'Сетка', on: _grid, onTap: () => setState(() => _grid = true)),
                            const SizedBox(width: 4),
                            _Seg(icon: Icons.view_agenda_outlined, label: 'Список', on: !_grid, onTap: () => setState(() => _grid = false)),
                          ]),
                        ),
                      ),
                      const SizedBox(width: 8),
                      ActionChip(
                        label: const Text('Сегодня'),
                        onPressed: () {
                          _picked = null;
                          if (_start != null) _load(null);
                        },
                      ),
                    ]),
                    const SizedBox(height: 14),
                    RiskGrid(risks: week.risks, compact: true),
                    const SizedBox(height: 14),
                    FilledButton.tonalIcon(
                      onPressed: _openPlan,
                      icon: const Icon(Icons.auto_awesome),
                      label: const Text('Разложить задачи'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(56),
                        backgroundColor: pal.container,
                        foregroundColor: pal.onContainer,
                      ),
                    ),
                    if (_grid) ...[
                      const SizedBox(height: 14),
                      _HourGrid(
                        week: week,
                        days: days,
                        today: today,
                        picked: picked,
                        onPick: (d) => setState(() => _picked = d),
                      ),
                      const SizedBox(height: 12),
                      _DayCard(week: week, day: picked, today: today),
                    ] else
                      WeekView(week: week, today: today),
                  ],
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}

class _Seg extends StatelessWidget {
  const _Seg({required this.icon, required this.label, required this.on, required this.onTap});

  final IconData icon;
  final String label;
  final bool on;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    return Expanded(
      child: Material(
        color: on ? pal.surface : Colors.transparent,
        borderRadius: BorderRadius.circular(16),
        elevation: on ? 1 : 0,
        child: InkWell(
          borderRadius: BorderRadius.circular(16),
          onTap: onTap,
          child: Container(
            constraints: const BoxConstraints(minHeight: 48),
            alignment: Alignment.center,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              Icon(icon, size: 20, color: on ? pal.text : pal.text2),
              const SizedBox(width: 6),
              Flexible(
                child: Text(label,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(color: on ? pal.text : pal.text2)),
              ),
            ]),
          ),
        ),
      ),
    );
  }
}

/// Minutes of [e] on [day] inside the hour [h] (0 to 23).
int _minutesInHour(CalEvent e, Wall day, int h) {
  final hourStart = Wall(day.year, day.month, day.day, h);
  final hourEnd = h == 23 ? day.dayStart.addDays(1) : Wall(day.year, day.month, day.day, h + 1);
  final s = e.start.minutesSince(hourStart) > 0 ? e.start : hourStart;
  final en = e.end.minutesSince(hourEnd) < 0 ? e.end : hourEnd;
  return en.minutesSince(s).clamp(0, 60);
}

class _HourGrid extends StatelessWidget {
  const _HourGrid({required this.week, required this.days, required this.today, required this.picked, required this.onPick});

  final WeekData week;
  final List<Wall> days;
  final Wall today, picked;
  final ValueChanged<Wall> onPick;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: pal.surface,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: pal.line),
      ),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Нагрузка по часам', style: theme.textTheme.titleLarge),
        const SizedBox(height: 8),
        LayoutBuilder(builder: (context, c) {
          const left = 48.0, right = 42.0;
          final gridW = c.maxWidth - left - right;
          final cell = (gridW - 2 * (_gridHours - 1)) / _gridHours;
          return Column(children: [
            SizedBox(
              height: 18,
              child: Stack(children: [
                for (final h in [8, 12, 16, 20, 24])
                  Positioned(
                    left: left + (h - _gridFrom) * (cell + 2) - (h == 24 ? 10 : 0),
                    child: Text('$h', style: theme.textTheme.bodySmall?.copyWith(fontSize: 12)),
                  ),
              ]),
            ),
            for (final d in days)
              _row(context, d, cell, left, b, pal, theme),
          ]);
        }),
      ]),
    );
  }

  Widget _row(BuildContext context, Wall d, double cell, double left, Brightness b, Palette pal, ThemeData theme) {
    final events = week.eventsOn(d).where((e) => !e.allDay).toList();
    var total = 0;
    for (final e in events) {
      for (var h = 0; h < 24; h++) {
        total += _minutesInHour(e, d, h);
      }
    }
    final isToday = d.sameDay(today), isPicked = d.sameDay(picked);
    final totalH = (total / 60).round();
    return InkWell(
      borderRadius: BorderRadius.circular(8),
      onTap: () => onPick(d),
      child: Container(
        constraints: const BoxConstraints(minHeight: 44),
        decoration: isPicked ? BoxDecoration(color: pal.container.withValues(alpha: 0.5), borderRadius: BorderRadius.circular(8)) : null,
        child: Row(children: [
          SizedBox(
            width: left,
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
              Text(
                weekdayShort[d.weekday - 1][0].toUpperCase() + weekdayShort[d.weekday - 1].substring(1),
                style: theme.textTheme.titleSmall?.copyWith(color: isToday ? pal.primary : null, height: 1.1),
              ),
              Text('${d.day}', style: theme.textTheme.bodySmall?.copyWith(fontSize: 13, height: 1.1)),
            ]),
          ),
          for (var i = 0; i < _gridHours; i++) ...[
            Builder(builder: (context) {
              CalEvent? hit;
              var best = 0;
              for (final e in events) {
                final m = _minutesInHour(e, d, _gridFrom + i);
                if (m > best) {
                  best = m;
                  hit = e;
                }
              }
              return Container(
                width: cell,
                height: 24,
                decoration: BoxDecoration(
                  color: hit == null || best < 15 ? pal.surface2 : categoryColor(hit.category, b),
                  borderRadius: BorderRadius.circular(4),
                ),
              );
            }),
            if (i < _gridHours - 1) const SizedBox(width: 2),
          ],
          SizedBox(
            width: 42,
            child: Text('$totalH ч',
                textAlign: TextAlign.right,
                style: theme.textTheme.labelMedium?.copyWith(
                    color: totalH >= 8 ? (b == Brightness.dark ? const Color(0xFFFFDD73) : const Color(0xFF6A4600)) : pal.text2,
                    fontWeight: FontWeight.w700)),
          ),
        ]),
      ),
    );
  }
}

class _DayCard extends StatelessWidget {
  const _DayCard({required this.week, required this.day, required this.today});

  final WeekData week;
  final Wall day, today;

  @override
  Widget build(BuildContext context) {
    return WeekView(week: WeekData(from: day, to: day.addDays(1), events: week.events, warnings: const [], risks: week.risks), today: today);
  }
}
