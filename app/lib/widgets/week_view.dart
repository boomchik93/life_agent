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
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 6),
            decoration: BoxDecoration(
              color: pal.surface,
              borderRadius: BorderRadius.circular(20),
              border: Border.all(color: pal.line),
            ),
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
                    padding: const EdgeInsets.only(bottom: 8),
                    child: Text(
                      'Нет событий',
                      style: theme.textTheme.bodySmall,
                    ),
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
    return Container(
      decoration: BoxDecoration(
        border: Border(top: BorderSide(color: pal.line)),
      ),
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: IntrinsicHeight(
        child: Row(
          children: [
            SizedBox(
              width: 104,
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
              width: 4,
              constraints: const BoxConstraints(minHeight: 22),
              decoration: BoxDecoration(
                color: categoryColor(event.category, theme.brightness),
                borderRadius: BorderRadius.circular(2),
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
