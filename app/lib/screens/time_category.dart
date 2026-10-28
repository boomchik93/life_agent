import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../format.dart';
import '../screen_time.dart';
import '../theme.dart';
import '../time_period.dart';
import '../timetrack.dart';
import '../widgets/error_view.dart';
import '../widgets/time_ui.dart';
import 'time_apps.dart';

class _CategoryData {
  _CategoryData(this.detail, this.screen);

  final CategoryDetail detail;

  /// The phone's apps of the category; null when the usage access is missing.
  final ScreenTime? screen;
}

/// One category: the total, the days, what the time went on, the apps and the records.
class CategoryScreen extends StatefulWidget {
  const CategoryScreen({super.key, required this.ctx, required this.category, required this.period, this.weekShift = 0});

  final TimeCtx ctx;
  final String category;
  final TimePeriod period;
  final int weekShift;

  @override
  State<CategoryScreen> createState() => _CategoryScreenState();
}

class _CategoryScreenState extends State<CategoryScreen> {
  late Future<_CategoryData> _future = _load();

  (DateTime, DateTime) get _range => periodRange(widget.period, widget.ctx.today, weekShift: widget.weekShift);

  Future<_CategoryData> _load() async {
    final c = widget.ctx;
    final (from, to) = _range;
    final detail = await c.time.timeCategory(widget.category, from, to);
    ScreenTime? screen;
    try {
      if (await c.usage.hasAccess()) {
        screen = await c.screenTime(from, to);
      }
    } catch (_) {
      // The apps are an extra: the rest of the screen still shows.
    }
    return _CategoryData(detail, screen);
  }

  void _reload() => setState(() => _future = _load());

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final cat = widget.category;
    final accent = timeColor(context, cat);
    final (from, to) = _range;
    return TimeScaffold(
      title: timeCategoryNames[cat]!,
      child: FutureBuilder<_CategoryData>(
        future: _future,
        builder: (context, snap) {
          if (snap.connectionState != ConnectionState.done) {
            return const Padding(padding: EdgeInsets.all(16), child: LoadingList(label: 'Собираю данные…'));
          }
          if (snap.hasError) return ErrorView(error: snap.error!, onRetry: _reload);
          final d = snap.data!.detail;
          final st = snap.data!.screen;
          final apps = st?.inCategory(cat) ?? const <AppUse>[];
          final screenMin = apps.fold(0, (s, a) => s + a.min);
          final delta = d.totalMin - d.previousMin;
          final points = _points(d.days);
          return ListView(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
            children: [
              TimeCard(children: [
                Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Text(periodTitle(from, to), style: theme.textTheme.bodySmall),
                      Text(hm(d.totalMin), style: theme.textTheme.headlineLarge?.copyWith(fontSize: 36)),
                    ]),
                  ),
                  Column(crossAxisAlignment: CrossAxisAlignment.end, children: [
                    DeltaText(delta),
                    Text('${d.sharePct}% всего времени', style: theme.textTheme.bodySmall),
                  ]),
                ]),
              ]),
              TimeCard(children: [
                Text(widget.period == TimePeriod.week ? 'По дням' : 'По неделям', style: theme.textTheme.titleLarge),
                const SizedBox(height: 12),
                SizedBox(
                  height: 150,
                  child: CustomPaint(
                    size: const Size(double.infinity, 150),
                    painter: _LinePainter(points, accent, pal.line, pal.surface, pal.text3),
                  ),
                ),
                const SizedBox(height: 6),
                if (widget.period == TimePeriod.week)
                  Row(children: [
                    for (var i = 0; i < 7; i++)
                      Expanded(
                        child: Text(weekdayShort[i][0].toUpperCase() + weekdayShort[i].substring(1),
                            textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
                      ),
                  ]),
              ]),
              if (d.parts.isNotEmpty)
                TimeCard(children: [
                  Text('На что ушло', style: theme.textTheme.titleLarge),
                  const SizedBox(height: 8),
                  for (final p in d.parts)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        Row(children: [
                          Expanded(child: Text(p.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall)),
                          Text('${hm(p.minutes)} · ${d.totalMin == 0 ? 0 : (p.minutes * 100 / d.totalMin).round()}%',
                              style: theme.textTheme.bodyMedium?.copyWith(color: pal.text2)),
                        ]),
                        const SizedBox(height: 6),
                        FillBar(fraction: d.totalMin == 0 ? 0 : p.minutes / d.totalMin, color: accent),
                      ]),
                    ),
                ]),
              if (st != null && apps.isNotEmpty)
                TimeCard(children: [
                  Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
                    Flexible(child: Text('Приложения', style: theme.textTheme.titleLarge)),
                    const SizedBox(width: 8),
                    Flexible(child: Text('${hm(screenMin)} за экраном', textAlign: TextAlign.right, style: theme.textTheme.bodySmall)),
                  ]),
                  const SizedBox(height: 4),
                  for (final a in apps.take(4))
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Row(children: [
                        AppBadge(a.pkg),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                            Text(appLabel(a.pkg), maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
                            const SizedBox(height: 6),
                            FillBar(fraction: screenMin == 0 ? 0 : a.min / screenMin, color: accent),
                          ]),
                        ),
                        const SizedBox(width: 12),
                        Column(crossAxisAlignment: CrossAxisAlignment.end, children: [
                          Text(hm(a.min), style: theme.textTheme.titleSmall),
                          Text('${screenMin == 0 ? 0 : (a.min * 100 / screenMin).round()}%', style: theme.textTheme.bodySmall),
                        ]),
                      ]),
                    ),
                  if (d.totalMin > screenMin)
                    Text('Остальные ${hm(d.totalMin - screenMin)} вне экрана: календарь и таймер.', style: theme.textTheme.bodySmall),
                  const SizedBox(height: 8),
                  SizedBox(
                    width: double.infinity,
                    child: FilledButton.tonal(
                      style: FilledButton.styleFrom(backgroundColor: pal.container, foregroundColor: pal.onContainer),
                      onPressed: () => Navigator.of(context).push(MaterialPageRoute(
                        builder: (_) => AppsScreen(ctx: widget.ctx, category: cat, period: widget.period, weekShift: widget.weekShift, categoryTotalMin: d.totalMin),
                      )),
                      child: const Text('Все приложения категории'),
                    ),
                  ),
                ]),
              if (d.records.isNotEmpty)
                TimeCard(padding: const EdgeInsets.fromLTRB(16, 12, 16, 8), children: [
                  Text('Записи', style: theme.textTheme.titleLarge),
                  const SizedBox(height: 4),
                  for (final r in d.records.take(12))
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Row(children: [
                        Container(width: 4, height: 36, decoration: BoxDecoration(color: accent, borderRadius: BorderRadius.circular(2))),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                            Text(r.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
                            Text('${_when(r.start, widget.ctx.today)}–${_hmTime(r.end)} · ${hm(r.minutes)}', style: theme.textTheme.bodySmall),
                          ]),
                        ),
                        Text(r.source == 'timer' ? 'Таймер' : 'Календарь', style: theme.textTheme.bodySmall),
                      ]),
                    ),
                ]),
              if (st == null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text('Чтобы увидеть приложения, разреши доступ к статистике использования.', style: theme.textTheme.bodySmall),
                ),
            ],
          );
        },
      ),
    );
  }

  /// The values of the chart: days, or weeks for a long period.
  List<double> _points(List<DayMinutes> days) {
    final v = [for (final d in days) (d.byCategory[widget.category] ?? 0) / 60];
    if (widget.period == TimePeriod.week) return v;
    final out = <double>[];
    for (var i = 0; i < v.length; i += 7) {
      out.add(v.sublist(i, math.min(i + 7, v.length)).fold(0.0, (s, x) => s + x));
    }
    return out;
  }

  static String _hmTime(DateTime d) => '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';

  static String _when(DateTime d, DateTime today) {
    final day = DateTime(d.year, d.month, d.day);
    final diff = today.difference(day).inDays;
    final label = diff == 0 ? 'Сегодня' : diff == 1 ? 'Вчера' : '${d.day}.${d.month.toString().padLeft(2, '0')}';
    return '$label, ${_hmTime(d)}';
  }
}

class _LinePainter extends CustomPainter {
  _LinePainter(this.values, this.color, this.grid, this.dot, this.label);

  final List<double> values;
  final Color color, grid, dot, label;

  @override
  void paint(Canvas canvas, Size size) {
    const padL = 34.0;
    final w = size.width - padL, h = size.height - 8;
    final maxV = math.max(1.0, (values.isEmpty ? 1 : values.reduce(math.max)).ceilToDouble());
    final gridPaint = Paint()
      ..color = grid
      ..strokeWidth = 1;
    for (final f in [0.0, 0.5, 1.0]) {
      final y = 4 + h * (1 - f);
      canvas.drawLine(Offset(padL, y), Offset(size.width, y), gridPaint);
      final tp = TextPainter(
        text: TextSpan(text: f == 0 ? '0' : '${(maxV * f).round()} ч', style: TextStyle(fontSize: 12, color: label, fontFamily: fontText)),
        textDirection: TextDirection.ltr,
      )..layout();
      tp.paint(canvas, Offset(0, y - tp.height / 2));
    }
    if (values.isEmpty) return;
    final n = values.length;
    Offset pt(int i) => Offset(padL + (n == 1 ? w / 2 : w * i / (n - 1)), 4 + h * (1 - values[i] / maxV));
    final line = Path()..moveTo(pt(0).dx, pt(0).dy);
    for (var i = 1; i < n; i++) {
      line.lineTo(pt(i).dx, pt(i).dy);
    }
    final area = Path.from(line)
      ..lineTo(pt(n - 1).dx, 4 + h)
      ..lineTo(pt(0).dx, 4 + h)
      ..close();
    canvas.drawPath(area, Paint()..color = color.withValues(alpha: 0.2));
    canvas.drawPath(
        line,
        Paint()
          ..color = color
          ..style = PaintingStyle.stroke
          ..strokeWidth = 3
          ..strokeJoin = StrokeJoin.round
          ..strokeCap = StrokeCap.round);
    if (n <= 14) {
      for (var i = 0; i < n; i++) {
        canvas.drawCircle(pt(i), 4.5, Paint()..color = dot);
        canvas.drawCircle(
            pt(i),
            4.5,
            Paint()
              ..color = color
              ..style = PaintingStyle.stroke
              ..strokeWidth = 3);
      }
    }
  }

  @override
  bool shouldRepaint(_LinePainter old) => old.values != values || old.color != color;
}
