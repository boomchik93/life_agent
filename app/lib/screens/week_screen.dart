import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../models.dart';
import '../widgets/error_view.dart';
import '../widgets/risk_indicator.dart';
import '../widgets/week_view.dart';

/// Any week of the calendar, with arrows to move between weeks.
class WeekScreen extends StatefulWidget {
  const WeekScreen({super.key, required this.api, this.now});

  final LifeApi api;
  final DateTime Function()? now;

  @override
  State<WeekScreen> createState() => _WeekScreenState();
}

class _WeekScreenState extends State<WeekScreen> {
  Wall? _start; // null: current week
  late Future<WeekData> _future = widget.api.week();
  WeekData? _shown;

  void _load(Wall? start) {
    final future = widget.api.week(start: start);
    setState(() {
      _start = start;
      _future = future;
    });
  }

  void _move(int weeks) {
    final shown = _shown;
    if (shown == null) return;
    _load(shown.from.addDays(7 * weeks));
  }

  @override
  Widget build(BuildContext context) {
    final today = wallNow(widget.now?.call());
    return Scaffold(
      appBar: AppBar(
        title: FutureBuilder<WeekData>(
          future: _future,
          builder: (_, s) => Text(s.hasData ? weekRange(s.data!.from, s.data!.to) : 'Неделя'),
        ),
        actions: [
          IconButton(tooltip: 'Прошлая неделя', icon: const Icon(Icons.chevron_left), onPressed: () => _move(-1)),
          IconButton(tooltip: 'Следующая неделя', icon: const Icon(Icons.chevron_right), onPressed: () => _move(1)),
        ],
      ),
      body: FutureBuilder<WeekData>(
        future: _future,
        builder: (context, snap) {
          if (snap.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snap.hasError) {
            return ErrorView(error: snap.error!, onRetry: () => _load(_start));
          }
          final week = snap.data!;
          _shown = week;
          return RefreshIndicator(
            onRefresh: () async {
              _load(_start);
              await _future.catchError((_) => week);
            },
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                RiskGrid(risks: week.risks),
                WeekView(week: week, today: today),
              ],
            ),
          );
        },
      ),
    );
  }
}
