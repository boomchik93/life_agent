import 'dart:async';
import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';
import 'screen_time.dart';
import 'timetrack.dart';
import 'usage.dart';

enum TimePeriod { week, month, quarter }

const timePeriodNames = {
  TimePeriod.week: 'Неделя',
  TimePeriod.month: 'Месяц',
  TimePeriod.quarter: '3 месяца',
};

/// The days [from, to) of a period. The week is the calendar week shifted by
/// [weekShift]; month and quarter end today.
(DateTime, DateTime) periodRange(TimePeriod p, DateTime today, {int weekShift = 0}) {
  final t = DateTime(today.year, today.month, today.day);
  switch (p) {
    case TimePeriod.week:
      final mon = DateTime(t.year, t.month, t.day - (t.weekday - 1) + 7 * weekShift);
      return (mon, DateTime(mon.year, mon.month, mon.day + 7));
    case TimePeriod.month:
      return (DateTime(t.year, t.month, t.day - 29), DateTime(t.year, t.month, t.day + 1));
    case TimePeriod.quarter:
      return (DateTime(t.year, t.month, t.day - 89), DateTime(t.year, t.month, t.day + 1));
  }
}

/// What a "Время" screen needs: the server, the phone's usage, the names of
/// apps already placed and the clock.
class TimeCtx {
  TimeCtx({required this.api, required this.time, required this.usage, this.now});

  final LifeApi api;
  final TimeApi time;
  final UsageSource usage;
  final DateTime Function()? now;
  final Map<String, NameCategory> names = {};

  DateTime get today {
    final n = now?.call() ?? DateTime.now();
    return DateTime(n.year, n.month, n.day);
  }

  static const _kNames = 'app_names';

  /// How long the places of the apps kept on the phone are trusted: the
  /// server learns new names, and the user may move an app elsewhere.
  static const namesKeep = Duration(days: 7);

  bool _restored = false;

  /// The phone's apps over [from, to), each in its category. The places found
  /// are kept on the phone, so a new start does not ask about every app again.
  Future<ScreenTime> screenTime(DateTime from, DateTime to) async {
    await _restoreNames();
    final s = await loadScreenTime(usage: usage, api: api, from: from, to: to, names: names);
    unawaited(keepNames());
    return s;
  }

  Future<void> _restoreNames() async {
    if (_restored) return;
    _restored = true;
    try {
      final raw = (await SharedPreferences.getInstance()).getString(_kNames);
      if (raw == null) return;
      final j = jsonDecode(raw) as Map<String, dynamic>;
      final at = DateTime.parse(j['at'] as String);
      if (DateTime.now().difference(at) > namesKeep) return;
      (j['names'] as Map<String, dynamic>).forEach((pkg, v) {
        final l = v as List;
        names.putIfAbsent(pkg, () => NameCategory(category: l[0] as String, confidence: (l[1] as num).toDouble(), resolved: true));
      });
    } catch (_) {
      // Nothing kept, or unreadable: the server is asked.
    }
  }

  /// Saves the places that are sure; an app no layer could place is asked again.
  Future<void> keepNames() async {
    try {
      final sure = {
        for (final e in names.entries)
          if (e.value.resolved && e.value.category != 'unknown') e.key: [e.value.category, e.value.confidence],
      };
      await (await SharedPreferences.getInstance())
          .setString(_kNames, jsonEncode({'at': DateTime.now().toIso8601String(), 'names': sure}));
    } catch (_) {
      // Only a lost speed-up.
    }
  }
}

/// "26 октября – 1 ноября" for the days [from, to).
String periodTitle(DateTime from, DateTime to) {
  const months = [
    'января', 'февраля', 'марта', 'апреля', 'мая', 'июня',
    'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'
  ];
  final last = to.subtract(const Duration(days: 1));
  return '${from.day} ${months[from.month - 1]} – ${last.day} ${months[last.month - 1]}';
}
