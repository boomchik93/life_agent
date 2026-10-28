import 'dart:async';

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../screen_time.dart';
import '../theme.dart';
import '../time_period.dart';
import '../timetrack.dart';
import '../usage.dart';
import '../widgets/time_ui.dart';

/// One app: the minutes by day, the sessions and the category the user can change.
class AppScreen extends StatefulWidget {
  const AppScreen({super.key, required this.ctx, required this.use, required this.period, this.categoryTotalMin = 0});

  final TimeCtx ctx;
  final AppUse use;
  final TimePeriod period;
  final int categoryTotalMin;

  @override
  State<AppScreen> createState() => _AppScreenState();
}

class _AppScreenState extends State<AppScreen> {
  late String? _category = widget.use.category;
  bool _saving = false, _changed = false;
  String? _error;

  Future<void> _choose(String c) async {
    if (_saving || c == _category) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.ctx.time.setAppCategory(widget.use.pkg, appCategoryOf(c));
      widget.ctx.names[widget.use.pkg] = NameCategory(category: appCategoryOf(c), confidence: 1, resolved: true);
      unawaited(widget.ctx.keepNames());
      if (mounted) {
        setState(() {
          _category = c;
          _changed = true;
          _saving = false;
        });
      }
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pal = Palette.of(context);
    final use = widget.use;
    final (from, to) = periodRange(widget.period, widget.ctx.today);
    final days = <DateTime>[];
    if (widget.period == TimePeriod.week) {
      for (var i = 0; i < 7; i++) {
        days.add(DateTime(from.year, from.month, from.day + i));
      }
    }
    final maxDay = days.fold<double>(1, (m, d) => math.max(m, use.byDay[dayKey(d)] ?? 0));
    final sessions = [...use.sessions]..sort((a, b) => b.start.compareTo(a.start));
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) Navigator.of(context).pop(_changed);
      },
      child: TimeScaffold(
        title: appLabel(use.pkg),
        leading: AppBadge(use.pkg),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
          children: [
            TimeCard(children: [
              Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(periodTitle(from, to), style: theme.textTheme.bodySmall),
                    Text(hm(use.min), style: theme.textTheme.headlineLarge?.copyWith(fontSize: 32)),
                  ]),
                ),
                DeltaText(use.delta),
              ]),
              if (_category != null && widget.categoryTotalMin > 0)
                Text('${(use.min * 100 / widget.categoryTotalMin).round()}% времени категории «${timeCategoryNames[_category]}»',
                    style: theme.textTheme.bodySmall),
            ]),
            if (days.isNotEmpty)
              TimeCard(children: [
                Text('По дням', style: theme.textTheme.titleLarge),
                const SizedBox(height: 12),
                SizedBox(
                  height: 120,
                  child: Row(crossAxisAlignment: CrossAxisAlignment.end, children: [
                    for (final d in days)
                      Expanded(
                        child: Column(mainAxisAlignment: MainAxisAlignment.end, children: [
                          Text((use.byDay[dayKey(d)] ?? 0) < 1 ? '' : '${(use.byDay[dayKey(d)]!).round()}',
                              style: theme.textTheme.bodySmall?.copyWith(fontSize: 12)),
                          const SizedBox(height: 4),
                          Container(
                            width: 22,
                            height: math.max(4, 80 * (use.byDay[dayKey(d)] ?? 0) / maxDay),
                            decoration: BoxDecoration(
                              color: _category == null ? pal.primary : timeColor(context, _category!),
                              borderRadius: BorderRadius.circular(6),
                            ),
                          ),
                        ]),
                      ),
                  ]),
                ),
                const SizedBox(height: 6),
                Row(children: [
                  for (final d in days)
                    Expanded(
                      child: Text(weekdayShort[d.weekday - 1][0].toUpperCase() + weekdayShort[d.weekday - 1].substring(1),
                          textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
                    ),
                ]),
              ]),
            if (sessions.isNotEmpty)
              TimeCard(padding: const EdgeInsets.fromLTRB(16, 12, 16, 8), children: [
                Text('Сессии', style: theme.textTheme.titleLarge),
                const SizedBox(height: 4),
                for (final s in sessions.take(6))
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Row(children: [
                      Expanded(
                        child: Text('${weekdayLong[s.start.weekday - 1]}, ${_t(s.start)}–${_t(s.end)}', style: theme.textTheme.titleSmall),
                      ),
                      Text(hm(math.max(1, s.minutes)), style: theme.textTheme.bodyMedium),
                    ]),
                  ),
              ]),
            TimeCard(children: [
              Text('К какой категории относится', style: theme.textTheme.titleLarge),
              const SizedBox(height: 12),
              Wrap(spacing: 8, runSpacing: 8, children: [
                for (final c in timeCategories.where((c) => c != 'chores' && c != 'rest'))
                  CatChip(category: c, selected: _category == c, onTap: () => _choose(c)),
              ]),
              const SizedBox(height: 8),
              Text('Выбор запоминается: это приложение будет попадать в эту категорию и дальше.', style: theme.textTheme.bodySmall),
              if (_error != null) Padding(padding: const EdgeInsets.only(top: 8), child: Text(_error!, style: TextStyle(color: theme.colorScheme.error))),
            ]),
          ],
        ),
      ),
    );
  }

  String _t(DateTime d) => '${d.hour.toString().padLeft(2, '0')}:${d.minute.toString().padLeft(2, '0')}';
}
