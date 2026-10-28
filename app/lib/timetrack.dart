/// The "Время" tab: where the time goes (calendar and timer, by category), the
/// activities the user follows and the timer. The server does the sums.
abstract interface class TimeApi {
  Future<TimeOverview> timeOverview(DateTime from, DateTime to, {bool compare = false});
  Future<CategoryDetail> timeCategory(String category, DateTime from, DateTime to);
  Future<ActivitiesData> activities({String range = 'month'});

  /// Remembers the category the user chose for an app; it wins over any guess.
  Future<void> setAppCategory(String name, String category);

  Future<List<RiskDay>> riskHistory({int days = 7});
}

/// The categories of the tab: the ones of the calendar.
const timeCategories = ['study', 'work', 'hobby', 'rest', 'chores', 'other'];

const timeCategoryNames = {
  'study': 'Учёба',
  'work': 'Работа',
  'hobby': 'Хобби',
  'rest': 'Отдых',
  'chores': 'Дела',
  'other': 'Другое',
  'career': 'Карьера',
  'sport': 'Спорт',
};

Map<String, int> _ints(Object? j) => {
      for (final e in ((j as Map?) ?? const {}).entries) e.key as String: (e.value as num).toInt(),
    };

class DayMinutes {
  const DayMinutes(this.date, this.byCategory);

  final String date; // YYYY-MM-DD
  final Map<String, int> byCategory;

  int get total => byCategory.values.fold(0, (s, v) => s + v);
}

class TimeOverview {
  const TimeOverview({
    required this.totalMin,
    required this.byCategory,
    required this.previous,
    required this.days,
    required this.screen,
    required this.screenUnknownMin,
  });

  final int totalMin;
  final Map<String, int> byCategory;
  final Map<String, int>? previous;
  final List<DayMinutes> days;

  /// Minutes in apps by category, and the minutes no category was found for.
  final Map<String, int> screen;
  final int screenUnknownMin;

  factory TimeOverview.fromJson(Map<String, dynamic> j) => TimeOverview(
        totalMin: (j['total_min'] as num).toInt(),
        byCategory: _ints(j['by_category']),
        previous: j['previous'] == null ? null : _ints(j['previous']),
        days: [
          for (final d in (j['days'] as List))
            DayMinutes((d as Map)['date'] as String, _ints(d['by_category'])),
        ],
        screen: _ints(j['screen']),
        screenUnknownMin: (j['screen_unknown_min'] as num?)?.toInt() ?? 0,
      );
}

class TimePart {
  const TimePart(this.name, this.minutes);

  final String name;
  final int minutes;
}

class TimeRecord {
  const TimeRecord({required this.title, required this.start, required this.end, required this.minutes, required this.source});

  final String title, source; // source: timer or calendar
  final DateTime start, end;
  final int minutes;
}

class CategoryDetail {
  const CategoryDetail({
    required this.category,
    required this.totalMin,
    required this.previousMin,
    required this.sharePct,
    required this.days,
    required this.parts,
    required this.records,
    required this.screenMin,
  });

  final String category;
  final int totalMin, previousMin, sharePct, screenMin;
  final List<DayMinutes> days;
  final List<TimePart> parts;
  final List<TimeRecord> records;

  factory CategoryDetail.fromJson(Map<String, dynamic> j) {
    final cat = j['category'] as String;
    return CategoryDetail(
      category: cat,
      totalMin: (j['total_min'] as num).toInt(),
      previousMin: (j['previous_min'] as num).toInt(),
      sharePct: (j['share_pct'] as num).toInt(),
      screenMin: (j['screen_min'] as num).toInt(),
      days: [
        for (final d in (j['days'] as List)) DayMinutes((d as Map)['date'] as String, _ints(d['by_category'])),
      ],
      parts: [
        for (final p in (j['parts'] as List)) TimePart((p as Map)['name'] as String, (p['minutes'] as num).toInt()),
      ],
      records: [
        for (final r in (j['records'] as List))
          TimeRecord(
            title: (r as Map)['title'] as String,
            start: DateTime.parse(r['start'] as String),
            end: DateTime.parse(r['end'] as String),
            minutes: (r['minutes'] as num).toInt(),
            source: r['source'] as String,
          ),
      ],
    );
  }
}

/// One of the four goals followed by itself: the apps, the calendar, the
/// watch, GitHub and LeetCode feed it, nobody marks anything.
class AutoActivity {
  const AutoActivity({
    required this.key,
    required this.name,
    required this.goalMinutes,
    this.streak = 0,
    this.activeDays = 0,
    this.goalPercent = -1,
    this.totalMin = 0,
    this.heat = const [],
  });

  final String key, name; // key: study, career, sport, work
  final int goalMinutes, streak, activeDays, goalPercent, totalMin;
  final List<HeatDay> heat;

  factory AutoActivity.fromJson(Map<String, dynamic> j) => AutoActivity(
        key: j['key'] as String,
        name: j['name'] as String,
        goalMinutes: (j['goal_minutes'] as num).toInt(),
        streak: (j['streak'] as num?)?.toInt() ?? 0,
        activeDays: (j['active_days'] as num?)?.toInt() ?? 0,
        goalPercent: (j['goal_percent'] as num?)?.toInt() ?? -1,
        totalMin: (j['total_min'] as num?)?.toInt() ?? 0,
        heat: [
          for (final h in ((j['heat'] as List?) ?? const []))
            HeatDay((h as Map)['minutes'] == null ? 0 : (h['minutes'] as num).toInt(),
                active: h['active'] == true, future: h['future'] == true),
        ],
      );
}

class HeatDay {
  const HeatDay(this.minutes, {this.active = false, this.future = false});

  final int minutes;

  /// Something happened that day even without minutes (contributions, solved problems).
  final bool active;
  final bool future;
}

class ActivitiesData {
  const ActivitiesData(this.weeks, this.activities);

  final int weeks;
  final List<AutoActivity> activities;

  factory ActivitiesData.fromJson(Map<String, dynamic> j) => ActivitiesData(
        (j['weeks'] as num).toInt(),
        [for (final a in (j['activities'] as List)) AutoActivity.fromJson(a as Map<String, dynamic>)],
      );
}

/// The colours of the four risks on one day, for the strip and the sparklines.
class RiskDay {
  const RiskDay({required this.date, required this.sleep, required this.overload, required this.noRest, required this.state, required this.worst});

  final String date;
  final String sleep, overload, noRest, state, worst; // green, yellow, red, gray

  factory RiskDay.fromJson(Map<String, dynamic> j) => RiskDay(
        date: j['date'] as String,
        sleep: j['sleep'] as String,
        overload: j['overload'] as String,
        noRest: j['no_rest'] as String,
        state: j['state'] as String,
        worst: j['worst'] as String,
      );
}
