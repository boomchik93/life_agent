import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../api.dart';
import '../theme.dart';
import '../time_period.dart';
import '../timetrack.dart';
import '../widgets/loadable.dart';
import '../widgets/time_ui.dart';

/// The four goals, followed by themselves: the apps of the phone and the Mac,
/// the calendar, the watch, GitHub and LeetCode feed the streak, the share of
/// the norm and the calendar of days. Nothing is marked by hand.
class ActivitiesTab extends StatefulWidget {
  const ActivitiesTab({super.key, required this.ctx, this.reload});

  final TimeCtx ctx;

  /// Bumped when the data may have changed.
  final ValueListenable<int>? reload;

  @override
  State<ActivitiesTab> createState() => _ActivitiesTabState();
}

class _ActivitiesTabState extends State<ActivitiesTab> {
  String _range = 'month';
  final _data = Loadable<ActivitiesData>();

  @override
  void initState() {
    super.initState();
    widget.reload?.addListener(_reload);
    _load();
  }

  @override
  void dispose() {
    widget.reload?.removeListener(_reload);
    _data.dispose();
    super.dispose();
  }

  /// Asks for the range shown; [other] when it is not the one on the screen.
  void _load({bool other = false}) {
    final api = widget.ctx.api;
    final kept = api is CachedReads ? (api as CachedReads).keptActivities(range: _range) : null;
    _data.load(widget.ctx.time.activities(range: _range), kept: kept, clear: other);
  }

  void _reload() => _load();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      SegRow<String>(
        value: _range,
        options: const [('week', 'Неделя'), ('month', 'Месяц'), ('year', 'Год')],
        onChanged: (r) {
          setState(() => _range = r);
          _load(other: true);
        },
      ),
      LoadableView<ActivitiesData>(
        source: _data,
        expand: false,
        placeholder: const Padding(padding: EdgeInsets.only(top: 12), child: LoadingList(label: 'Считаю активности…')),
        onRetry: _reload,
        builder: (context, data) {
          return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            for (final a in data.activities) _ActivityCard(activity: a, weeks: data.weeks),
            Padding(
              padding: const EdgeInsets.only(top: 16),
              child: Text(
                'Считается само: время в приложениях телефона и компьютера, календарь, тренировки с часов, GitHub и LeetCode. Норму в неделю задают цели.',
                style: theme.textTheme.bodySmall?.copyWith(color: pal.text2),
              ),
            ),
          ]);
        },
      ),
    ]);
  }
}

IconData _iconOf(String key) => switch (key) {
      'study' => Icons.menu_book_outlined,
      'career' => Icons.trending_up,
      'sport' => Icons.fitness_center,
      _ => Icons.work_outline,
    };

class _ActivityCard extends StatelessWidget {
  const _ActivityCard({required this.activity, required this.weeks});

  final AutoActivity activity;
  final int weeks;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final a = activity;
    final c = timeColor(context, a.key);
    final goal = a.goalMinutes == 0 ? 'норма появится после недель данных' : 'норма ${hm(a.goalMinutes)} в неделю';
    return TimeCard(children: [
      Row(children: [
        Container(
          width: 44,
          height: 44,
          decoration: BoxDecoration(color: c, borderRadius: BorderRadius.circular(14)),
          child: Icon(_iconOf(a.key), size: 22, color: pal.bg),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(a.name, style: theme.textTheme.titleSmall),
            Text(goal, style: theme.textTheme.bodySmall),
          ]),
        ),
        Row(mainAxisSize: MainAxisSize.min, children: [
          Icon(Icons.local_fire_department_outlined, color: c),
          const SizedBox(width: 2),
          Text('${a.streak}', style: theme.textTheme.titleSmall),
        ]),
      ]),
      const SizedBox(height: 12),
      Row(children: [
        _Stat(value: '${a.activeDays}', label: _daysWord(a.activeDays)),
        _Stat(value: a.goalPercent < 0 ? '—' : '${a.goalPercent}%', label: 'от нормы'),
        _Stat(value: hm(a.totalMin), label: 'всего'),
      ]),
      const SizedBox(height: 12),
      Semantics(label: 'Календарь активности за $weeks недель', child: _Heat(heat: a.heat, weeks: weeks, color: c)),
      const SizedBox(height: 6),
      Row(mainAxisAlignment: MainAxisAlignment.end, children: [
        Text('Меньше', style: theme.textTheme.bodySmall),
        const SizedBox(width: 6),
        for (final alpha in [0.0, 0.35, 0.65, 1.0])
          Container(
            width: 14,
            height: 14,
            margin: const EdgeInsets.symmetric(horizontal: 2),
            decoration: BoxDecoration(color: alpha == 0 ? pal.surface2 : c.withValues(alpha: alpha), borderRadius: BorderRadius.circular(3)),
          ),
        const SizedBox(width: 6),
        Text('Больше', style: theme.textTheme.bodySmall),
      ]),
    ]);
  }

  static String _daysWord(int n) {
    final m10 = n % 10, m100 = n % 100;
    if (m10 == 1 && m100 != 11) return 'день был';
    if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return 'дня были';
    return 'дней были';
  }
}

class _Stat extends StatelessWidget {
  const _Stat({required this.value, required this.label});

  final String value, label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Expanded(
      child: Column(children: [
        FittedBox(fit: BoxFit.scaleDown, child: Text(value, style: theme.textTheme.titleLarge)),
        Text(label, style: theme.textTheme.bodySmall),
      ]),
    );
  }
}

/// Weeks as columns, Monday at the top; the shade is the minutes of the day.
class _Heat extends StatelessWidget {
  const _Heat({required this.heat, required this.weeks, required this.color});

  final List<HeatDay> heat;
  final int weeks;
  final Color color;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final theme = Theme.of(context);
    final maxMin = heat.fold<int>(1, (m, h) => math.max(m, h.minutes));
    Color shade(HeatDay h) {
      if (h.future) return Colors.transparent;
      if (h.minutes == 0) return h.active ? color.withValues(alpha: 0.35) : pal.surface2;
      final f = h.minutes / maxMin;
      return color.withValues(alpha: f < 0.34 ? 0.35 : f < 0.67 ? 0.65 : 1.0);
    }

    final labels = ['Пн', '', 'Ср', '', 'Пт', '', 'Вс'];
    return LayoutBuilder(builder: (context, c) {
      const gap = 2.0, left = 24.0;
      final cell = math.min(22.0, (c.maxWidth - left - gap * (weeks - 1)) / weeks);
      return Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        SizedBox(
          width: left,
          child: Column(children: [
            for (var d = 0; d < 7; d++)
              SizedBox(
                height: cell + gap,
                child: Text(labels[d], style: theme.textTheme.bodySmall?.copyWith(fontSize: math.min(11, cell))),
              ),
          ]),
        ),
        for (var w = 0; w < weeks; w++) ...[
          Column(children: [
            for (var d = 0; d < 7; d++)
              if (w * 7 + d < heat.length)
                Container(
                  width: cell,
                  height: cell,
                  margin: const EdgeInsets.only(bottom: gap),
                  decoration: BoxDecoration(color: shade(heat[w * 7 + d]), borderRadius: BorderRadius.circular(math.min(4, cell / 4))),
                ),
          ]),
          if (w < weeks - 1) const SizedBox(width: gap),
        ],
      ]);
    });
  }
}
