import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../format.dart';
import '../models.dart';
import '../theme.dart';
import 'motion.dart';
import 'risk_indicator.dart';

const _categoryNames = {'study': 'Учёба', 'work': 'Работа', 'sport': 'Спорт', 'hobby': 'Хобби', 'rest': 'Отдых', 'chores': 'Дела'};

/// "Дальше": the next event with a countdown and the day on a time line.
/// The main card of the home screen, on the hero gradient.
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
    int rank(RiskColor c) => switch (c) {
      RiskColor.red => 3,
      RiskColor.yellow => 2,
      RiskColor.green => 1,
      _ => 0,
    };
    return all.reduce((a, b) => rank(b.color) > rank(a.color) ? b : a);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final next = _next;
    final worst = _worst();
    // The card is dark in both themes: the light shades of the colours read on it.
    const onDark = Brightness.dark;
    final rc = riskColor(worst.color, onDark);
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

    final text = pal.onHero;
    final soft = pal.onHero.withValues(alpha: 0.78);
    return Container(
      margin: const EdgeInsets.only(bottom: 14),
      clipBehavior: Clip.antiAlias,
      decoration: heroDecoration(pal),
      child: Stack(
        children: [
          // A soft circle in the corner, for depth.
          Positioned(right: -60, top: -70, child: _Glow(size: 200, color: pal.onHero.withValues(alpha: 0.07))),
          Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Text(
                      'Дальше',
                      style: theme.textTheme.bodySmall?.copyWith(color: soft, fontWeight: FontWeight.w700, letterSpacing: 0.4),
                    ),
                    Flexible(
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                        decoration: BoxDecoration(color: pal.onHero.withValues(alpha: 0.14), borderRadius: BorderRadius.circular(16)),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(riskIcon(worst.color), size: 16, color: rc),
                            const SizedBox(width: 6),
                            Flexible(
                              child: Text(
                                worst.color == RiskColor.green ? 'Нагрузка в норме' : 'Нагрузка: ${riskLabel(worst.color).toLowerCase()}',
                                overflow: TextOverflow.ellipsis,
                                style: theme.textTheme.labelMedium?.copyWith(color: text, fontWeight: FontWeight.w700),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                if (next == null)
                  Row(
                    children: [
                      Icon(Icons.event_available, size: 32, color: soft),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Text('Ближайших событий нет', style: theme.textTheme.headlineMedium?.copyWith(color: text)),
                      ),
                    ],
                  )
                else
                  Row(
                    children: [
                      SizedBox(
                        width: 88,
                        height: 88,
                        child: Stack(
                          alignment: Alignment.center,
                          children: [
                            Grow(
                              value: (1 - (next.start.minutesSince(now).clamp(0, 60) / 60)).toDouble(),
                              ms: 900,
                              builder: (context, v) => SizedBox(
                                width: 88,
                                height: 88,
                                child: CircularProgressIndicator(
                                  value: v,
                                  strokeWidth: 7,
                                  strokeCap: StrokeCap.round,
                                  color: pal.accent,
                                  backgroundColor: pal.onHero.withValues(alpha: 0.16),
                                ),
                              ),
                            ),
                            Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                Text(
                                  countdown(next),
                                  style: const TextStyle(
                                    fontFamily: fontNumbers,
                                    fontWeight: FontWeight.w600,
                                    fontSize: 26,
                                    height: 1.1,
                                  ).copyWith(color: text),
                                ),
                                Text(unit(next), style: theme.textTheme.bodySmall?.copyWith(color: soft, fontSize: 12)),
                              ],
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(next.title, style: theme.textTheme.headlineMedium?.copyWith(color: text)),
                            const SizedBox(height: 4),
                            Text(
                              '${next.start.sameDay(now) ? '' : '${dayTitle(next.start.dayStart).split(',').first} · '}'
                              '${eventTime(next, next.start.dayStart)}'
                              '${_categoryNames[next.category] == null ? '' : ' · ${_categoryNames[next.category]}'}',
                              style: theme.textTheme.bodyMedium?.copyWith(color: soft),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                const SizedBox(height: 16),
                _Timeline(events: todays, now: now, from: _from, to: _to),
                const SizedBox(height: 10),
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Flexible(
                      child: Text(
                        'Занято ${minutesLabel(busy)} из ${_to - _from} ч дня',
                        style: theme.textTheme.bodySmall?.copyWith(color: soft),
                      ),
                    ),
                    Text(_eventsWord(todays.length), style: theme.textTheme.bodySmall?.copyWith(color: soft)),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  static String _eventsWord(int n) {
    if (n == 0) return 'нет событий';
    final m10 = n % 10, m100 = n % 100;
    final w = (m10 == 1 && m100 != 11)
        ? 'событие'
        : (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20))
        ? 'события'
        : 'событий';
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
    const b = Brightness.dark; // on the hero gradient
    final span = (to - from) * 60.0;
    double frac(Wall w) => (((w.hour - from) * 60 + w.minute) / span).clamp(0.0, 1.0);
    final style = Theme.of(context).textTheme.bodySmall?.copyWith(color: pal.onHero.withValues(alpha: 0.7), fontSize: 12);
    const marks = [8, 12, 16, 20];
    // Painted, not laid out from the width: the card also sits in columns
    // that ask for their intrinsic height, which a LayoutBuilder cannot give.
    return Semantics(
      label: 'Шкала дня с $from до $to часов, событий: ${events.length}',
      child: Column(
        children: [
          SizedBox(
            height: 20,
            width: double.infinity,
            child: CustomPaint(
              painter: _TimelinePainter(
                track: pal.onHero.withValues(alpha: 0.14),
                mark: pal.accent,
                bars: [
                  for (final e in events)
                    (frac(e.start), frac(e.end.sameDay(e.start) ? e.end : e.start.dayStart.addDays(1)), categoryColor(e.category, b)),
                ],
                now: now.hour >= from && now.hour < to ? frac(now) : null,
              ),
            ),
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              if (marks.first > from) Spacer(flex: marks.first - from),
              for (var i = 0; i < marks.length; i++)
                Expanded(
                  flex: (i + 1 < marks.length ? marks[i + 1] : to) - marks[i],
                  child: Text('${marks[i]}', style: style, maxLines: 1, overflow: TextOverflow.clip, softWrap: false),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _TimelinePainter extends CustomPainter {
  _TimelinePainter({required this.track, required this.mark, required this.bars, required this.now});

  final Color track, mark;

  /// Start and end as shares of the line, and the colour.
  final List<(double, double, Color)> bars;

  /// Where now is on the line; null outside it.
  final double? now;

  @override
  void paint(Canvas canvas, Size size) {
    final w = size.width, h = size.height;
    canvas.drawRRect(RRect.fromLTRBR(0, 0, w, h, const Radius.circular(10)), Paint()..color = track);
    for (final (a, z, color) in bars) {
      final left = a * w;
      final right = (z * w).clamp(left + 4, w);
      canvas.drawRRect(RRect.fromLTRBR(left, 3, right, h - 3, const Radius.circular(6)), Paint()..color = color);
    }
    final x = now;
    if (x != null) {
      final r = RRect.fromLTRBR(x * w - 2, -2, x * w + 2, h + 2, const Radius.circular(2));
      canvas.drawRRect(r, Paint()
        ..color = mark.withValues(alpha: 0.6)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 3));
      canvas.drawRRect(r, Paint()..color = mark);
    }
  }

  @override
  bool shouldRepaint(_TimelinePainter old) =>
      old.track != track || old.mark != mark || old.now != now || !listEquals(old.bars, bars);
}

class _Glow extends StatelessWidget {
  const _Glow({required this.size, required this.color});

  final double size;
  final Color color;

  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(shape: BoxShape.circle, color: color),
  );
}
