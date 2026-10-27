import 'package:life_usage/life_usage.dart';

import 'api.dart';
import 'models.dart';

/// A category with how sure we are of it.
class Guess {
  const Guess(this.category, this.confidence);

  final String category;
  final double confidence;
}

/// Under this confidence a category counts as "unknown" (PLAN.md section 14).
const confidenceThreshold = 0.7;

/// Layer 1, the phone's part: the category the Android system gives an app.
/// Productivity apps could be study, work or career, so that mapping stays
/// under the threshold and the server (dictionary, then model) decides.
Guess? androidMetaGuess(int androidCategory) => switch (androidCategory) {
      0 => const Guess('leisure', 0.9), // game
      1 => const Guess('leisure', 0.8), // audio
      2 => const Guess('leisure', 0.8), // video
      4 => const Guess('social', 0.9),
      5 => const Guess('leisure', 0.75), // news
      7 => const Guess('work', 0.6), // productivity: not sure
      _ => null,
    };

/// Layer 2: a calendar event with a category colours the time inside it.
/// Household chores and "other" say nothing about what was on the screen.
String? contextCategory(String eventCategory) => switch (eventCategory) {
      'study' => 'study',
      'work' => 'work',
      'hobby' || 'rest' => 'leisure',
      _ => null,
    };

/// A calendar event reduced to what the context layer needs.
class ContextEvent {
  const ContextEvent(this.start, this.end, this.category);

  final DateTime start, end;
  final String category; // an activity category
}

/// The minutes of one category on one day, as they are sent.
class UsageRow {
  const UsageRow({required this.category, required this.minutes, required this.nightMinutes, required this.confidence});

  final String category;
  final int minutes, nightMinutes;
  final double confidence;

  Map<String, dynamic> toJson() => {
        'category': category,
        'minutes': minutes,
        'night_minutes': nightMinutes,
        'confidence': confidence,
      };
}

class UsageDay {
  const UsageDay({required this.date, required this.rows});

  final String date; // YYYY-MM-DD
  final List<UsageRow> rows;

  int get totalMinutes => rows.fold(0, (s, r) => s + r.minutes);

  int get unknownMinutes => rows.where((r) => r.category == 'unknown').fold(0, (s, r) => s + r.minutes);

  /// The body of `POST /v1/activity/daily`: categories and numbers only.
  Map<String, dynamic> toJson(String source) => {
        'source': source,
        'date': date,
        'rows': [for (final r in rows) r.toJson()],
      };
}

/// The answer of `POST /v1/categorize` for one name.
class NameCategory {
  const NameCategory({required this.category, required this.confidence, required this.resolved});

  final String category;
  final double confidence;

  /// False: no layer could answer today; the name stays unknown for now.
  final bool resolved;
}

typedef Categorizer = Future<Map<String, NameCategory>> Function(List<String> names, {required bool llm});

class _Seg {
  _Seg(this.pkg, this.start, this.end);

  final String pkg;
  final DateTime start, end;
}

String _ymd(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

double _minutes(DateTime a, DateTime b) => b.difference(a).inMilliseconds / 60000;

double _overlap(DateTime s1, DateTime e1, DateTime s2, DateTime e2) {
  final s = s1.isAfter(s2) ? s1 : s2, e = e1.isBefore(e2) ? e1 : e2;
  return e.isAfter(s) ? _minutes(s, e) : 0;
}

class _Acc {
  double minutes = 0, night = 0, confWeight = 0;
}

/// Turns the sessions of one day into minutes per category. The order of the
/// layers is the one of PLAN.md: the system's category, the dictionary, the
/// calendar context, and only then the model for what is still unknown.
/// Only categories and numbers come out; the package names go to the server
/// (to [categorize]) only for apps no local layer could place.
Future<UsageDay> summarizeUsage({
  required DateTime day,
  required List<AppSession> sessions,
  required List<ContextEvent> events,
  required Categorizer categorize,
  int sleepStartHour = 23,
  int sleepEndHour = 7,
}) async {
  final dayStart = DateTime(day.year, day.month, day.day);
  final dayEnd = DateTime(day.year, day.month, day.day + 1);
  final nightA = (dayStart, DateTime(day.year, day.month, day.day, sleepEndHour));
  final nightB = (DateTime(day.year, day.month, day.day, sleepStartHour), dayEnd);

  final segs = <_Seg>[];
  final androidCat = <String, int>{};
  for (final s in sessions) {
    final a = s.start.isBefore(dayStart) ? dayStart : s.start;
    final b = s.end.isAfter(dayEnd) ? dayEnd : s.end;
    if (b.isAfter(a)) {
      segs.add(_Seg(s.pkg, a, b));
      androidCat[s.pkg] = s.androidCategory;
    }
  }

  // Layer 1: the system's category, when it is sure.
  final known = <String, Guess>{};
  for (final pkg in androidCat.keys) {
    final g = androidMetaGuess(androidCat[pkg]!);
    if (g != null && g.confidence >= confidenceThreshold) known[pkg] = g;
  }
  // Layer 1 again: the server's dictionary and cache, no model yet.
  final rest = [for (final p in androidCat.keys) if (!known.containsKey(p)) p];
  final settled = <String>{}; // asked before and answered "unknown": not asked again
  if (rest.isNotEmpty) {
    final got = await categorize(rest, llm: false);
    for (final p in rest) {
      final g = got[p];
      if (g == null || !g.resolved) continue;
      if (g.category != 'unknown' && g.confidence >= confidenceThreshold) {
        known[p] = Guess(g.category, g.confidence);
      } else {
        settled.add(p);
      }
    }
  }

  // Layer 2: the calendar context colours the time of apps still unplaced.
  final acc = <String, _Acc>{};
  void add(String category, double minutes, double night, double conf) {
    final a = acc.putIfAbsent(category, _Acc.new);
    a.minutes += minutes;
    a.night += night;
    a.confWeight += minutes * conf;
  }

  double nightOf(DateTime s, DateTime e) =>
      _overlap(s, e, nightA.$1, nightA.$2) + _overlap(s, e, nightB.$1, nightB.$2);

  final leftover = <String, List<(DateTime, DateTime)>>{}; // unplaced time per package
  for (final seg in segs) {
    final g = known[seg.pkg];
    if (g != null) {
      add(g.category, _minutes(seg.start, seg.end), nightOf(seg.start, seg.end), g.confidence);
      continue;
    }
    // Cut the segment by the context events; each cut part takes its category.
    var cursor = seg.start;
    final cuts = [for (final e in events) if (e.end.isAfter(seg.start) && e.start.isBefore(seg.end)) e]
      ..sort((a, b) => a.start.compareTo(b.start));
    for (final e in cuts) {
      final s = e.start.isAfter(cursor) ? e.start : cursor;
      final t = e.end.isBefore(seg.end) ? e.end : seg.end;
      if (!t.isAfter(s)) continue;
      if (s.isAfter(cursor)) leftover.putIfAbsent(seg.pkg, () => []).add((cursor, s));
      add(e.category, _minutes(s, t), nightOf(s, t), 0.9);
      cursor = t;
    }
    if (seg.end.isAfter(cursor)) leftover.putIfAbsent(seg.pkg, () => []).add((cursor, seg.end));
  }

  // Layer 4: the model, only for names that still have unplaced time.
  final ask = [for (final p in leftover.keys) if (!settled.contains(p)) p];
  final modelGuess = <String, Guess>{};
  if (ask.isNotEmpty) {
    final got = await categorize(ask, llm: true);
    for (final p in ask) {
      final g = got[p];
      if (g != null && g.resolved && g.category != 'unknown' && g.confidence >= confidenceThreshold) {
        modelGuess[p] = Guess(g.category, g.confidence);
      }
    }
  }
  for (final entry in leftover.entries) {
    final g = modelGuess[entry.key];
    for (final (s, e) in entry.value) {
      add(g?.category ?? 'unknown', _minutes(s, e), nightOf(s, e), g?.confidence ?? 0);
    }
  }

  final rows = <UsageRow>[];
  for (final e in acc.entries) {
    final minutes = e.value.minutes.round();
    if (minutes <= 0) continue;
    rows.add(UsageRow(
      category: e.key,
      minutes: minutes,
      nightMinutes: e.value.night.round().clamp(0, minutes),
      confidence: double.parse((e.value.confWeight / e.value.minutes).clamp(0, 1).toStringAsFixed(2)),
    ));
  }
  rows.sort((a, b) => a.category.compareTo(b.category));
  return UsageDay(date: _ymd(day), rows: rows);
}

/// What the app needs from the system: access, and the sessions of a period.
abstract class UsageSource {
  Future<bool> hasAccess();
  Future<void> openAccessSettings();
  Future<List<AppSession>> sessions(DateTime from, DateTime to);
}

class PluginUsageSource implements UsageSource {
  @override
  Future<bool> hasAccess() => LifeUsage.hasAccess();

  @override
  Future<void> openAccessSettings() => LifeUsage.openAccessSettings();

  @override
  Future<List<AppSession>> sessions(DateTime from, DateTime to) => LifeUsage.sessions(from, to);
}

/// Sends yesterday's and today's sums. The raw sessions live only inside this
/// call: nothing is kept after it.
class UsageSync {
  UsageSync({required this.api, required this.source, DateTime Function()? now}) : _now = now ?? DateTime.now;

  final LifeApi api;
  final UsageSource source;
  final DateTime Function() _now;

  /// Returns the days sent, or null with a reason in [skipped].
  Future<({int sent, String? skipped, int unknownPercent})> run() async {
    if (!await source.hasAccess()) {
      return (sent: 0, skipped: 'Нет доступа к истории использования.', unknownPercent: 0);
    }
    final now = _now();
    final today = DateTime(now.year, now.month, now.day);
    var sent = 0, total = 0, unknown = 0;
    for (final day in [DateTime(today.year, today.month, today.day - 1), today]) {
      final end = DateTime(day.year, day.month, day.day + 1);
      final sessions = await source.sessions(day, end.isAfter(now) ? now : end);
      final events = await _contextEvents(day);
      final usage = await summarizeUsage(
        day: day,
        sessions: sessions,
        events: events,
        categorize: (names, {required llm}) => api.categorize(names, llm: llm),
      );
      if (usage.rows.isEmpty) continue;
      await api.sendActivityDay(usage, source: 'android_usage');
      sent++;
      total += usage.totalMinutes;
      unknown += usage.unknownMinutes;
    }
    return (
      sent: sent,
      skipped: sent == 0 ? 'За эти дни нет данных об использовании.' : null,
      unknownPercent: total == 0 ? 0 : (100 * unknown / total).round(),
    );
  }

  Future<List<ContextEvent>> _contextEvents(DateTime day) async {
    try {
      final week = await api.week(start: Wall(day.year, day.month, day.day));
      final out = <ContextEvent>[];
      for (final e in week.events) {
        final c = contextCategory(e.category);
        if (c == null || e.allDay) continue;
        out.add(ContextEvent(
          DateTime(e.start.year, e.start.month, e.start.day, e.start.hour, e.start.minute),
          DateTime(e.end.year, e.end.month, e.end.day, e.end.hour, e.end.minute),
          c,
        ));
      }
      return out;
    } on ApiException {
      return const []; // no context is better than no summary
    }
  }
}
