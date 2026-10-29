import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../platform.dart';
import '../response_cache.dart';
import '../screen_time.dart';
import '../theme.dart';
import '../time_period.dart';
import '../timetrack.dart';
import '../usage.dart';
import '../widgets/loadable.dart';
import '../widgets/time_ui.dart';
import 'time_activities.dart';
import 'time_apps.dart';
import 'time_category.dart';

/// The "Время" tab: where the time goes (calendar and timer by category, the
/// apps of the phone) and the activities the user follows.
class TimeScreen extends StatefulWidget {
  const TimeScreen({super.key, required this.api, this.usage, this.now, this.reload});

  final LifeApi api;
  final UsageSource? usage;
  final DateTime Function()? now;

  /// Bumped by the home screen when the data may have changed.
  final ValueListenable<int>? reload;

  @override
  State<TimeScreen> createState() => _TimeScreenState();
}

class _Overview {
  _Overview(this.time, this.screen, {this.appsLater = false});

  final TimeOverview time;

  /// The phone's apps; null when the usage access is missing.
  final ScreenTime? screen;

  /// The apps are still being read: a kept answer of the server shows first.
  final bool appsLater;
}

class _TimeScreenState extends State<TimeScreen> {
  late final TimeCtx? _ctx = widget.api is TimeApi ? TimeCtx(api: widget.api, time: widget.api as TimeApi, usage: widget.usage ?? deviceUsageSource(), now: widget.now) : null;
  int _tab = 0; // 0: where the time goes, 1: activities
  TimePeriod _period = TimePeriod.week;
  int _weekShift = 0;
  final _overview = Loadable<_Overview>();
  final _activitiesReload = ValueNotifier(0);

  @override
  void initState() {
    super.initState();
    widget.reload?.addListener(_onReload);
    if (_ctx != null) _load();
  }

  @override
  void dispose() {
    widget.reload?.removeListener(_onReload);
    _overview.dispose();
    _activitiesReload.dispose();
    super.dispose();
  }

  void _onReload() {
    if (_ctx == null) return;
    _load();
    _activitiesReload.value++;
  }

  (DateTime, DateTime) get _range => periodRange(_period, _ctx!.today, weekShift: _weekShift);

  bool get _compare => _period != TimePeriod.quarter;

  /// Asks for the period shown; [other] when it is not the one on the screen.
  Future<void> _load({bool other = false}) {
    final c = _ctx!;
    final (from, to) = _range;
    final api = c.api;
    final kept = api is CachedReads ? (api as CachedReads).keptTimeOverview(from, to, compare: _compare) : null;
    return _overview.load(_fetch(c, from, to), kept: kept == null ? null : Cached(_Overview(kept.value, null, appsLater: true), kept.at), clear: other);
  }

  /// The server and the phone are asked at the same time.
  Future<_Overview> _fetch(TimeCtx c, DateTime from, DateTime to) async {
    final time = c.time.timeOverview(from, to, compare: _compare);
    final screen = _screenTime(c, from, to);
    return _Overview(await time, await screen);
  }

  Future<ScreenTime?> _screenTime(TimeCtx c, DateTime from, DateTime to) async {
    try {
      if (await c.usage.hasAccess()) return await c.screenTime(from, to);
    } catch (_) {
      // The apps are an extra: the diagram works without them.
    }
    return null;
  }

  void _reload() => _load();

  void _shift(int d) {
    if (_weekShift + d > 0) return;
    setState(() => _weekShift += d);
    _load(other: true);
  }

  Future<void> _refresh() {
    if (_tab == 1) {
      _activitiesReload.value++;
      return Future.value();
    }
    return _load();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final c = _ctx;
    if (c == null) {
      return Center(child: Text('Этот раздел недоступен.', style: theme.textTheme.bodyLarge));
    }
    final (from, to) = _range;
    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 20, 16, 24),
        children: [
          Text('Время', style: theme.textTheme.headlineLarge),
          if (_tab == 0)
            _period == TimePeriod.week
                ? Row(
                    children: [
                      IconButton(tooltip: 'Предыдущая неделя', icon: const Icon(Icons.chevron_left), onPressed: () => _shift(-1)),
                      Expanded(
                        child: Text(
                          periodTitle(from, to),
                          textAlign: TextAlign.center,
                          style: theme.textTheme.bodyMedium?.copyWith(color: pal.text2, fontWeight: FontWeight.w600),
                        ),
                      ),
                      IconButton(tooltip: 'Следующая неделя', icon: const Icon(Icons.chevron_right), onPressed: _weekShift == 0 ? null : () => _shift(1)),
                    ],
                  )
                : Padding(
                    padding: const EdgeInsets.symmetric(vertical: 14),
                    child: Text(
                      periodTitle(from, to),
                      style: theme.textTheme.bodyMedium?.copyWith(color: pal.text2, fontWeight: FontWeight.w600),
                    ),
                  )
          else
            const SizedBox(height: 12),
          SegRow<int>(value: _tab, options: const [(0, 'Где уходит время'), (1, 'Активности')], onChanged: (t) => setState(() => _tab = t)),
          const SizedBox(height: 12),
          if (_tab == 1)
            ActivitiesTab(ctx: c, reload: _activitiesReload)
          else ...[
            SegRow<TimePeriod>(
              value: _period,
              options: [for (final p in TimePeriod.values) (p, timePeriodNames[p]!)],
              onChanged: (p) {
                setState(() => _period = p);
                _load(other: true);
              },
            ),
            LoadableView<_Overview>(
              source: _overview,
              expand: false,
              placeholder: const LoadingList(label: 'Считаю время…'),
              onRetry: _reload,
              builder: (context, o) {
                if (o.time.totalMin == 0 && !o.appsLater && (o.screen == null || o.screen!.apps.isEmpty)) {
                  return Padding(
                    padding: const EdgeInsets.only(top: 48),
                    child: Text('За этот период нет данных: ни событий в календаре, ни записей таймера.', textAlign: TextAlign.center, style: theme.textTheme.bodyLarge),
                  );
                }
                return _Content(ctx: c, data: o, period: _period, weekShift: _weekShift, from: from, onReload: _reload);
              },
            ),
          ],
        ],
      ),
    );
  }
}

const _order = ['study', 'work', 'sport', 'hobby', 'chores', 'rest', 'other'];

class _Content extends StatelessWidget {
  const _Content({required this.ctx, required this.data, required this.period, required this.weekShift, required this.from, required this.onReload});

  final TimeCtx ctx;
  final _Overview data;
  final TimePeriod period;
  final int weekShift;
  final DateTime from;
  final VoidCallback onReload;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    final t = data.time;
    final cats = t.byCategory;
    final total = t.totalMin;
    final groups = [
      for (final g in _order)
        if ((cats[g] ?? 0) > 0) g,
    ];
    final prev = t.previous;
    final periodWord = period == TimePeriod.week ? 'прошлой неделе' : 'прошлому периоду';

    // Bars: days of a week, or weeks for the long periods.
    final buckets = <(String, Map<String, int>)>[];
    if (period == TimePeriod.week) {
      for (var i = 0; i < t.days.length; i++) {
        buckets.add((weekdayShort[i % 7][0].toUpperCase() + weekdayShort[i % 7].substring(1), t.days[i].byCategory));
      }
    } else {
      for (var i = 0; i < t.days.length; i += 7) {
        final m = <String, int>{};
        for (final d in t.days.sublist(i, math.min(i + 7, t.days.length))) {
          d.byCategory.forEach((k, v) => m[k] = (m[k] ?? 0) + v);
        }
        final d0 = DateTime(from.year, from.month, from.day + i);
        buckets.add(('${d0.day}.${d0.month.toString().padLeft(2, '0')}', m));
      }
    }
    final maxBucket = buckets.fold<int>(1, (m, x) => math.max(m, x.$2.values.fold(0, (s, v) => s + v)));
    final avg = buckets.isEmpty ? 0.0 : total / buckets.length;
    final todayIdx = period == TimePeriod.week && weekShift == 0 ? ctx.today.weekday - 1 : -1;

    String? insight;
    if (prev != null) {
      String? top;
      var topDelta = 0;
      for (final g in _order) {
        final d = (cats[g] ?? 0) - (prev[g] ?? 0);
        if (d.abs() > topDelta.abs()) {
          top = g;
          topDelta = d;
        }
      }
      if (top != null && topDelta.abs() >= 30) {
        insight = '${timeCategoryNames[top]}: на ${hm(topDelta.abs())} ${topDelta > 0 ? 'больше' : 'меньше'}, чем на $periodWord';
      }
    }

    final screen = data.screen;
    final apps = screen?.apps ?? const <AppUse>[];
    final screenTotal = apps.fold(0, (s, a) => s + a.min);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (total > 0)
          TimeCard(
            children: [
              Text('Где уходит время', style: theme.textTheme.titleLarge),
              const SizedBox(height: 16),
              Center(
                child: Semantics(
                  label: 'Круговая диаграмма: ${[for (final g in groups) '${timeCategoryNames[g]} ${(cats[g]! * 100 / total).round()}%'].join(', ')}',
                  child: SizedBox(
                    width: 200,
                    height: 200,
                    child: TweenAnimationBuilder<double>(
                      key: ValueKey('${period.name}$weekShift$total'),
                      tween: Tween(begin: 0, end: 1),
                      duration: const Duration(milliseconds: 700),
                      curve: Curves.easeOutCubic,
                      builder: (context, v, child) => CustomPaint(painter: _DonutPainter([for (final g in groups) (cats[g]!.toDouble(), categoryColor(g, b))], pal.surface2, v), child: child),
                      child: Center(
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            SizedBox(
                              width: 110,
                              child: FittedBox(
                                fit: BoxFit.scaleDown,
                                child: Text(
                                  hm(total),
                                  style: const TextStyle(fontFamily: fontNumbers, fontWeight: FontWeight.w600, fontSize: 22),
                                ),
                              ),
                            ),
                            Text(
                              period == TimePeriod.week
                                  ? 'за неделю'
                                  : period == TimePeriod.month
                                  ? 'за месяц'
                                  : 'за 3 месяца',
                              style: theme.textTheme.bodySmall,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 12),
              for (final g in groups)
                InkWell(
                  borderRadius: BorderRadius.circular(12),
                  onTap: () => Navigator.of(context).push(
                    MaterialPageRoute(
                      builder: (_) => CategoryScreen(ctx: ctx, category: g, period: period, weekShift: weekShift),
                    ),
                  ),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 10),
                    child: Row(
                      children: [
                        Container(
                          width: 14,
                          height: 14,
                          decoration: BoxDecoration(color: categoryColor(g, b), shape: BoxShape.circle),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: FittedBox(
                            fit: BoxFit.scaleDown,
                            alignment: Alignment.centerLeft,
                            child: Text(timeCategoryNames[g]!, style: theme.textTheme.titleSmall),
                          ),
                        ),
                        const SizedBox(width: 8),
                        Flexible(
                          flex: 2,
                          child: FittedBox(
                            fit: BoxFit.scaleDown,
                            alignment: Alignment.centerRight,
                            child: Row(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Text(hm(cats[g]!), style: theme.textTheme.bodySmall?.copyWith(color: pal.text, fontSize: 15)),
                                if (prev != null) ...[const SizedBox(width: 8), DeltaText((cats[g] ?? 0) - (prev[g] ?? 0))],
                                const SizedBox(width: 8),
                                Text('${(cats[g]! * 100 / total).round()}%', style: theme.textTheme.bodySmall),
                              ],
                            ),
                          ),
                        ),
                        Icon(Icons.chevron_right, size: 20, color: pal.text3),
                      ],
                    ),
                  ),
                ),
              Text(
                prev != null ? 'Нажми на категорию, чтобы увидеть детали и приложения. Стрелки показывают изменение к $periodWord.' : 'Нажми на категорию, чтобы увидеть детали и приложения.',
                style: theme.textTheme.bodySmall,
              ),
            ],
          ),
        if (total > 0)
          TimeCard(
            children: [
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Flexible(child: Text(period == TimePeriod.week ? 'По дням' : 'По неделям', style: theme.textTheme.titleLarge)),
                  const SizedBox(width: 8),
                  Flexible(
                    child: Text(period == TimePeriod.week ? 'часов в день' : 'часов в неделю', textAlign: TextAlign.right, style: theme.textTheme.bodySmall),
                  ),
                ],
              ),
              const SizedBox(height: 16),
              SizedBox(
                height: 160,
                child: Stack(
                  children: [
                    Positioned(
                      left: 0,
                      right: 0,
                      bottom: 22 + 110 * (avg / maxBucket).clamp(0.0, 1.0),
                      child: CustomPaint(painter: _DashPainter(pal.text3), size: const Size(double.infinity, 1)),
                    ),
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.end,
                      children: [
                        for (var i = 0; i < buckets.length; i++)
                          Expanded(
                            child: Column(
                              mainAxisAlignment: MainAxisAlignment.end,
                              children: [
                                if (buckets.length <= 8)
                                  Text(
                                    buckets[i].$2.values.fold(0, (s, v) => s + v) == 0 ? '' : (buckets[i].$2.values.fold(0, (s, v) => s + v) / 60).toStringAsFixed(1).replaceAll('.', ','),
                                    style: theme.textTheme.bodySmall?.copyWith(fontSize: 12),
                                  ),
                                const SizedBox(height: 4),
                                _StackBar(byCategory: buckets[i].$2, maxMinutes: maxBucket, height: 110, brightness: b, width: buckets.length > 8 ? 12 : 22),
                                const SizedBox(height: 4),
                                SizedBox(
                                  height: 18,
                                  child: Text(
                                    buckets[i].$1,
                                    maxLines: 1,
                                    overflow: TextOverflow.clip,
                                    style: theme.textTheme.bodySmall?.copyWith(
                                      fontSize: buckets.length > 8 ? 9 : 14,
                                      color: i == todayIdx ? pal.primary : null,
                                      fontWeight: i == todayIdx ? FontWeight.w800 : null,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 8),
              Text('Пунктир: в среднем ${(avg / 60).toStringAsFixed(1).replaceAll('.', ',')} ч ${period == TimePeriod.week ? 'в день' : 'в неделю'}.', style: theme.textTheme.bodySmall),
            ],
          ),
        if (screen == null && !data.appsLater && isPhone)
          TimeCard(
            children: [
              Row(
                children: [
                  Icon(Icons.lock_outline, color: pal.text2),
                  const SizedBox(width: 12),
                  Expanded(child: Text('Нет доступа к статистике приложений', style: theme.textTheme.titleSmall)),
                ],
              ),
              const SizedBox(height: 4),
              Text('Диаграмма выше работает без неё. Разреши доступ, чтобы видеть приложения.', style: theme.textTheme.bodySmall),
              const SizedBox(height: 8),
              SizedBox(
                width: double.infinity,
                child: FilledButton.tonal(
                  style: FilledButton.styleFrom(backgroundColor: pal.container, foregroundColor: pal.onContainer),
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute(
                      builder: (_) => AppsScreen(ctx: ctx, category: null, period: period, weekShift: weekShift),
                    ),
                  ),
                  child: const Text('Как включить'),
                ),
              ),
            ],
          )
        else if (apps.isNotEmpty)
          TimeCard(
            children: [
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Flexible(child: Text('Приложения', style: theme.textTheme.titleLarge)),
                  const SizedBox(width: 8),
                  Flexible(
                    child: Text('${hm(screenTotal)} за экраном', textAlign: TextAlign.right, style: theme.textTheme.bodySmall),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              for (final a in apps.take(4))
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Row(
                    children: [
                      AppBadge(a.pkg),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(appLabel(a.pkg), maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
                            Text(a.category == null ? 'Без категории' : timeCategoryNames[a.category]!, style: theme.textTheme.bodySmall),
                          ],
                        ),
                      ),
                      Text(hm(a.min), style: theme.textTheme.titleSmall),
                    ],
                  ),
                ),
              const SizedBox(height: 4),
              SizedBox(
                width: double.infinity,
                child: FilledButton.tonal(
                  style: FilledButton.styleFrom(backgroundColor: pal.container, foregroundColor: pal.onContainer),
                  onPressed: () async {
                    await Navigator.of(context).push(
                      MaterialPageRoute(
                        builder: (_) => AppsScreen(ctx: ctx, category: null, period: period, weekShift: weekShift),
                      ),
                    );
                    onReload();
                  },
                  child: const Text('Все приложения'),
                ),
              ),
            ],
          ),
        if (insight != null)
          TimeCard(
            color: pal.container,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(color: pal.primary, borderRadius: BorderRadius.circular(14)),
                    child: Icon(Icons.lightbulb_outline, color: pal.onPrimary, size: 22),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(insight, style: theme.textTheme.titleSmall?.copyWith(color: pal.onContainer)),
                  ),
                ],
              ),
            ],
          ),
      ],
    );
  }
}

class _StackBar extends StatelessWidget {
  const _StackBar({required this.byCategory, required this.maxMinutes, required this.height, required this.brightness, this.width = 22});

  final Map<String, int> byCategory;
  final int maxMinutes;
  final double height, width;
  final Brightness brightness;

  @override
  Widget build(BuildContext context) {
    final total = byCategory.values.fold(0, (s, v) => s + v);
    if (total == 0) return SizedBox(width: width, height: 2);
    final h = math.max(4.0, height * total / maxMinutes);
    return ClipRRect(
      borderRadius: BorderRadius.circular(6),
      child: SizedBox(
        width: width,
        height: h,
        child: Column(
          children: [
            for (final g in _order.reversed)
              if ((byCategory[g] ?? 0) > 0)
                Expanded(
                  flex: byCategory[g]!,
                  child: Container(color: categoryColor(g, brightness)),
                ),
          ],
        ),
      ),
    );
  }
}

class _DashPainter extends CustomPainter {
  _DashPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 1.5;
    for (var x = 0.0; x < size.width; x += 8) {
      canvas.drawLine(Offset(x, 0), Offset(math.min(x + 4, size.width), 0), paint);
    }
  }

  @override
  bool shouldRepaint(_DashPainter old) => old.color != color;
}

class _DonutPainter extends CustomPainter {
  _DonutPainter(this.parts, this.track, this.progress);

  final List<(double, Color)> parts;
  final Color track;
  final double progress;

  @override
  void paint(Canvas canvas, Size size) {
    const stroke = 26.0;
    final rect = Rect.fromCircle(center: size.center(Offset.zero), radius: size.width / 2 - stroke / 2);
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = stroke
      ..color = track;
    canvas.drawArc(rect, 0, math.pi * 2, false, paint);
    final sum = parts.fold<double>(0, (s, p) => s + p.$1);
    if (sum == 0) return;
    const gap = 0.05;
    var a = -math.pi / 2;
    for (final p in parts) {
      final sweep = p.$1 / sum * math.pi * 2 * progress;
      paint.color = p.$2;
      if (sweep > gap) canvas.drawArc(rect, a + gap / 2, sweep - gap, false, paint);
      a += sweep;
    }
  }

  @override
  bool shouldRepaint(_DonutPainter old) => old.parts != parts || old.track != track || old.progress != progress;
}
