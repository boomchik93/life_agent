import 'package:flutter/material.dart';

import '../format.dart';
import '../models.dart';
import '../theme.dart';
import 'risk_indicator.dart';

const _categoryNames = {
  'study': 'Учёба',
  'work': 'Работа',
  'hobby': 'Хобби',
  'rest': 'Отдых',
  'chores': 'Дела',
};

/// "Дальше": the next event with a countdown and the day on a time line.
class NextCard extends StatelessWidget {
  const NextCard({super.key, required this.week, required this.now});

  final WeekData week;
  final Wall now;

  static const _from = 8, _to = 22; // hours of the time line

  CalEvent? get _next {
    CalEvent? best;
    for (final e in week.events) {
      if (e.allDay || e.end.minutesSince(now) <= 0) continue;
      if (best == null || e.start.minutesSince(best.start) < 0) best = e;
    }
    return best;
  }

  Risk _worst() {
    final all = [week.risks.sleep, week.risks.overload, week.risks.noRest, week.risks.state];
    int rank(RiskColor c) => switch (c) { RiskColor.red => 3, RiskColor.yellow => 2, RiskColor.green => 1, _ => 0 };
    return all.reduce((a, b) => rank(b.color) > rank(a.color) ? b : a);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final b = theme.brightness;
    final next = _next;
    final worst = _worst();
    final rc = riskColor(worst.color, b);
    final todays = week.events.where((e) => !e.allDay && e.start.sameDay(now)).toList();
    final busy = todays.fold<int>(0, (s, e) => s + e.end.minutesSince(e.start));

    String countdown(CalEvent e) {
      final m = e.start.minutesSince(now);
      if (m <= 0) return 'идёт';
      if (m < 60) return '$m';
      if (m < 24 * 60) return '${m ~/ 60}';
      return '${m ~/ (24 * 60)}';
    }

    String unit(CalEvent e) {
      final m = e.start.minutesSince(now);
      if (m <= 0) return 'сейчас';
      if (m < 60) return 'мин';
      if (m < 24 * 60) return 'ч';
      return 'дн';
    }

    final text = pal.onContainer;
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: pal.container, borderRadius: BorderRadius.circular(24)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
          Text('Дальше', style: theme.textTheme.bodySmall?.copyWith(color: text, fontWeight: FontWeight.w700)),
          Flexible(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
              decoration: BoxDecoration(color: rc.withValues(alpha: 0.18), borderRadius: BorderRadius.circular(16)),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                Icon(riskIcon(worst.color), size: 16, color: rc),
                const SizedBox(width: 6),
                Flexible(
                  child: Text(
                    worst.color == RiskColor.green ? 'Нагрузка в норме' : 'Нагрузка: ${riskLabel(worst.color).toLowerCase()}',
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.labelMedium?.copyWith(color: text, fontWeight: FontWeight.w700),
                  ),
                ),
              ]),
            ),
          ),
        ]),
        const SizedBox(height: 12),
        if (next == null)
          Text('Ближайших событий нет', style: theme.textTheme.headlineMedium?.copyWith(color: text))
        else
          Row(children: [
            SizedBox(
              width: 76,
              height: 76,
              child: Stack(alignment: Alignment.center, children: [
                SizedBox(
                  width: 76,
                  height: 76,
                  child: CircularProgressIndicator(
                    value: (1 - (next.start.minutesSince(now).clamp(0, 60) / 60)).toDouble(),
                    strokeWidth: 6,
                    color: pal.primary,
                    backgroundColor: pal.surface.withValues(alpha: 0.6),
                  ),
                ),
                Column(mainAxisSize: MainAxisSize.min, children: [
                  Text(countdown(next),
                      style: const TextStyle(fontFamily: fontNumbers, fontWeight: FontWeight.w600, fontSize: 20)
                          .copyWith(color: text)),
                  Text(unit(next), style: theme.textTheme.bodySmall?.copyWith(color: text, fontSize: 12)),
                ]),
              ]),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(next.title, style: theme.textTheme.headlineMedium?.copyWith(color: text)),
                const SizedBox(height: 4),
                Text(
                  '${next.start.sameDay(now) ? '' : '${dayTitle(next.start.dayStart).split(',').first} · '}'
                  '${eventTime(next, next.start.dayStart)}'
                  '${_categoryNames[next.category] == null ? '' : ' · ${_categoryNames[next.category]}'}',
                  style: theme.textTheme.bodyMedium?.copyWith(color: text),
                ),
              ]),
            ),
          ]),
        const SizedBox(height: 14),
        _Timeline(events: todays, now: now, from: _from, to: _to),
        const SizedBox(height: 10),
        Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
          Flexible(
            child: Text('Занято ${minutesLabel(busy)} из ${_to - _from} ч дня',
                style: theme.textTheme.bodySmall?.copyWith(color: text)),
          ),
          Text(_eventsWord(todays.length), style: theme.textTheme.bodySmall?.copyWith(color: text)),
        ]),
      ]),
    );
  }

  static String _eventsWord(int n) {
    if (n == 0) return 'нет событий';
    final m10 = n % 10, m100 = n % 100;
    final w = (m10 == 1 && m100 != 11) ? 'событие' : (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) ? 'события' : 'событий';
    return '$n $w';
  }
}

class _Timeline extends StatelessWidget {
  const _Timeline({required this.events, required this.now, required this.from, required this.to});

  final List<CalEvent> events;
  final Wall now;
  final int from, to;

  @override
  Widget build(BuildContext context) {
    final pal = Palette.of(context);
    final b = Theme.of(context).brightness;
    final span = (to - from) * 60.0;
    double frac(Wall w) => (((w.hour - from) * 60 + w.minute) / span).clamp(0.0, 1.0);
    final nowFrac = frac(now);
    final inDay = now.hour >= from && now.hour < to;
    return Semantics(
      label: 'Шкала дня с $from до $to часов, событий: ${events.length}',
      child: Column(children: [
        LayoutBuilder(builder: (context, c) {
          final w = c.maxWidth;
          return SizedBox(
            height: 20,
            child: Stack(children: [
              Positioned.fill(
                child: Container(decoration: BoxDecoration(color: pal.surface.withValues(alpha: 0.6), borderRadius: BorderRadius.circular(10))),
              ),
              for (final e in events)
                Positioned(
                  left: frac(e.start) * w,
                  width: ((frac(e.end.sameDay(e.start) ? e.end : e.start.dayStart.addDays(1)) - frac(e.start)) * w).clamp(4.0, w),
                  top: 2,
                  bottom: 2,
                  child: Container(decoration: BoxDecoration(color: categoryColor(e.category, b), borderRadius: BorderRadius.circular(6))),
                ),
              if (inDay)
                Positioned(left: nowFrac * w - 1.5, top: 0, bottom: 0, child: Container(width: 3, decoration: BoxDecoration(color: pal.primary, borderRadius: BorderRadius.circular(2)))),
            ]),
          );
        }),
        const SizedBox(height: 4),
        LayoutBuilder(builder: (context, c) {
          final style = Theme.of(context).textTheme.bodySmall?.copyWith(color: pal.onContainer, fontSize: 12);
          return SizedBox(
            height: 16,
            child: Stack(children: [
              for (final h in [8, 12, 16, 20])
                Positioned(left: (h - from) / (to - from) * c.maxWidth, child: Text('$h', style: style)),
            ]),
          );
        }),
      ]),
    );
  }
}
