import 'package:flutter/material.dart';

import '../format.dart';
import '../models.dart';

/// The events of a week grouped by day. Not scrollable by itself.
class WeekView extends StatelessWidget {
  const WeekView({super.key, required this.week, required this.today});

  final WeekData week;
  final Wall today;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final day in week.days) ...[
          Padding(
            padding: const EdgeInsets.only(top: 14, bottom: 4),
            child: Text(
              dayTitle(day),
              style: theme.textTheme.titleSmall?.copyWith(
                color: day.sameDay(today) ? theme.colorScheme.primary : null,
                fontWeight: day.sameDay(today) ? FontWeight.bold : null,
              ),
            ),
          ),
          if (week.eventsOn(day).isEmpty)
            Text('Нет событий', style: theme.textTheme.bodySmall?.copyWith(color: theme.hintColor))
          else
            for (final e in week.eventsOn(day)) _EventRow(event: e, day: day),
        ],
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
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 92,
            child: Text(eventTime(event, day), style: theme.textTheme.bodyMedium?.copyWith(color: theme.hintColor)),
          ),
          Expanded(child: Text(event.title)),
        ],
      ),
    );
  }
}
