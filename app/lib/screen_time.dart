import 'package:life_usage/life_usage.dart';

import 'api.dart';
import 'usage.dart';

/// The category of the "Время" tab an app category belongs to; null for what
/// says nothing (unknown, sleep). Video, music and games are rest, not a hobby.
String? timeCategoryOf(String appCategory) => switch (appCategory) {
      'study' => 'study',
      'work' || 'career' => 'work',
      'sport' => 'sport',
      'leisure' => 'rest',
      'social' => 'other',
      _ => null,
    };

/// The app category to store when the user picks a category of the tab.
String appCategoryOf(String timeCategory) => switch (timeCategory) {
      'study' => 'study',
      'work' => 'work',
      'sport' => 'sport',
      'hobby' || 'rest' => 'leisure',
      'chores' => 'work',
      _ => 'social',
    };

/// A readable name from an Android package: the last meaningful part.
String appLabel(String pkg) {
  final parts = pkg.split('.');
  var n = parts.last;
  if (n.length < 3 && parts.length > 1) n = parts[parts.length - 2];
  if (n.isEmpty) return pkg;
  return n[0].toUpperCase() + n.substring(1);
}

class AppSessionView {
  const AppSessionView(this.start, this.end);

  final DateTime start, end;

  int get minutes => end.difference(start).inMinutes;
}

/// One app over a period: minutes, the minutes of the period before, by day.
class AppUse {
  AppUse(this.pkg);

  final String pkg;
  double minutes = 0, previousMinutes = 0;
  final Map<String, double> byDay = {}; // YYYY-MM-DD
  final List<AppSessionView> sessions = [];

  /// The category of the tab; null: no category found for the app.
  String? category;

  int get min => minutes.round();
  int get delta => (minutes - previousMinutes).round();
}

String dayKey(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

class ScreenTime {
  ScreenTime(this.apps);

  final List<AppUse> apps;

  List<AppUse> inCategory(String? category) =>
      [for (final a in apps) if (category == null ? a.category != null : a.category == category) a];

  List<AppUse> get unassigned => [for (final a in apps) if (a.category == null) a];

  int minutesOf(String? category) => inCategory(category).fold(0, (s, a) => s + a.min);

  int get unassignedMinutes => unassigned.fold(0, (s, a) => s + a.min);
}

/// Reads the phone's sessions of [from, to) and of the period just before it,
/// and places every app in a category. Answers about names are kept in
/// [names], so each name is asked of the server once.
Future<ScreenTime> loadScreenTime({
  required UsageSource usage,
  required LifeApi api,
  required DateTime from,
  required DateTime to,
  required Map<String, NameCategory> names,
  bool compare = true,
}) async {
  final span = to.difference(from);
  final prevFrom = from.subtract(span);
  final sessions = await usage.sessions(compare ? prevFrom : from, to);
  final uses = <String, AppUse>{};
  final metaCat = <String, int>{};
  for (final s in sessions) {
    final use = uses.putIfAbsent(s.pkg, () => AppUse(s.pkg));
    metaCat[s.pkg] = s.androidCategory;
    if (!s.end.isAfter(from)) {
      use.previousMinutes += s.end.difference(s.start).inMilliseconds / 60000;
      continue;
    }
    // A session that crosses [from] counts from there.
    final start = s.start.isBefore(from) ? from : s.start;
    final end = s.end.isAfter(to) ? to : s.end;
    if (!end.isAfter(start)) continue;
    use.minutes += end.difference(start).inMilliseconds / 60000;
    final k = dayKey(start);
    use.byDay[k] = (use.byDay[k] ?? 0) + end.difference(start).inMilliseconds / 60000;
    use.sessions.add(AppSessionView(start, end));
  }

  final todo = [for (final p in uses.keys) if (!names.containsKey(p)) p];
  if (todo.isNotEmpty) {
    try {
      names.addAll(await api.categorize(todo, llm: false));
    } on ApiException {
      // The server is a layer, not a requirement: the system's category stays.
    }
  }
  for (final use in uses.values) {
    final n = names[use.pkg];
    String? app;
    if (n != null && n.resolved && n.category != 'unknown' && n.confidence >= confidenceThreshold) {
      app = n.category;
    } else {
      final g = androidMetaGuess(metaCat[use.pkg] ?? -1);
      if (g != null && g.confidence >= confidenceThreshold) app = g.category;
    }
    use.category = app == null ? null : timeCategoryOf(app);
  }
  final list = uses.values.where((a) => a.minutes >= 1).toList()..sort((a, b) => b.minutes.compareTo(a.minutes));
  return ScreenTime(list);
}
