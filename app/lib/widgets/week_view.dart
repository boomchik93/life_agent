import 'package:flutter/material.dart';

import '../format.dart';
import '../models.dart';
import '../theme.dart';

/// The events of a week grouped by day. Not scrollable by itself.
class WeekView extends StatelessWidget {
  const WeekView({super.key, required this.week, required this.today});

  final WeekData week;
  final Wall today;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final day in week.days)
          Container(
            margin: const EdgeInsets.only(top: 12),
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
            decoration: day.sameDay(today)
                ? cardDecoration(pal, radius: 20).copyWith(border: Border.all(color: pal.primary, width: 2))
                : cardDecoration(pal, radius: 20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(0, 10, 0, 8),
                  child: Row(
                    children: [
                      if (day.sameDay(today)) ...[
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 10,
                            vertical: 2,
                          ),
                          decoration: BoxDecoration(
                            color: pal.primary,
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Text(
                            'Сегодня',
                            style: theme.textTheme.labelMedium?.copyWith(
                              color: pal.onPrimary,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                        const SizedBox(width: 8),
                      ],
                      Flexible(
                        child: Text(
                          dayTitle(day),
                          style: theme.textTheme.titleSmall,
                        ),
                      ),
                    ],
                  ),
                ),
                if (week.eventsOn(day).isEmpty)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 6),
                    child: Row(children: [
                      Icon(Icons.wb_sunny_outlined, size: 18, color: pal.text3),
                      const SizedBox(width: 8),
                      Text('Нет событий', style: theme.textTheme.bodySmall),
                    ]),
                  )
                else
                  for (final e in week.eventsOn(day))
                    _EventRow(event: e, day: day),
              ],
            ),
          ),
      ],
    );
  }
}

class _EventRow extends StatelessWidget {
  const _EventRow({required this.event, required this.day});

  final CalEvent event;
  final Wall day;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final c = categoryColor(event.category, theme.brightness);
    return Container(
      margin: const EdgeInsets.only(top: 4),
      decoration: BoxDecoration(
        color: Color.alphaBlend(c.withValues(alpha: 0.10), pal.surface),
        borderRadius: BorderRadius.circular(14),
      ),
      padding: const EdgeInsets.fromLTRB(10, 10, 10, 10),
      child: IntrinsicHeight(
        child: Row(
          children: [
            SizedBox(
              width: 98,
              child: Text(
                eventTime(event, day),
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: pal.text2,
                  fontWeight: FontWeight.w600,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ),
            Container(
              width: 5,
              constraints: const BoxConstraints(minHeight: 22),
              decoration: BoxDecoration(
                color: c,
                borderRadius: BorderRadius.circular(3),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(event.title, style: theme.textTheme.titleSmall),
            ),
          ],
        ),
      ),
    );
  }
}
