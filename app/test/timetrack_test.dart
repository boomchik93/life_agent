import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/screen_time.dart';
import 'package:life_agent/screens/time_screen.dart';
import 'package:life_agent/theme.dart';
import 'package:life_agent/time_period.dart';
import 'package:life_agent/timetrack.dart';
import 'package:life_agent/usage.dart';
import 'package:life_usage/life_usage.dart';

import 'fakes.dart';

class FakeUsage implements UsageSource {
  bool access = true;
  List<AppSession> list = [];
  int opened = 0;

  @override
  Future<bool> hasAccess() async => access;
  @override
  Future<void> openAccessSettings() async => opened++;
  @override
  Future<List<AppSession>> sessions(DateTime from, DateTime to) async => list;
}

/// The server of the "Время" tab, in memory.
class FakeTimeApi extends FakeApi implements TimeApi {
  FakeTimeApi() : super();

  final appCategories = <String, String>{};
  final activityList = <AutoActivity>[
    AutoActivity(
      key: 'study',
      name: 'Учёба',
      goalMinutes: 480,
      streak: 3,
      activeDays: 4,
      goalPercent: 62,
      totalMin: 300,
      heat: [for (var i = 0; i < 84; i++) HeatDay(i % 5 == 0 ? 60 : 0, future: i > 80)],
    ),
    const AutoActivity(key: 'career', name: 'Карьера', goalMinutes: 0, activeDays: 2),
  ];

  @override
  Future<TimeOverview> timeOverview(DateTime from, DateTime to, {bool compare = false}) async => TimeOverview(
        totalMin: 600,
        byCategory: {'study': 360, 'work': 150, 'rest': 90},
        previous: compare ? {'study': 120, 'work': 150, 'rest': 120} : null,
        days: [for (var i = 0; i < to.difference(from).inDays; i++) DayMinutes('2026-10-${26 + i}', {'study': i == 1 ? 360 : 0})],
        screen: {'study': 100},
        screenUnknownMin: 0,
      );

  @override
  Future<CategoryDetail> timeCategory(String category, DateTime from, DateTime to) async => CategoryDetail(
        category: category,
        totalMin: 360,
        previousMin: 120,
        sharePct: 60,
        days: [for (var i = 0; i < to.difference(from).inDays; i++) DayMinutes('d$i', {category: i == 1 ? 360 : 0})],
        parts: const [TimePart('Лекции и семинары', 240), TimePart('Курсовая', 120)],
        records: [TimeRecord(title: 'Лекция', start: DateTime(2026, 10, 27, 10), end: DateTime(2026, 10, 27, 11, 30), minutes: 90, source: 'calendar')],
        screenMin: 100,
      );

  @override
  Future<ActivitiesData> activities({String range = 'month'}) async => ActivitiesData(12, activityList);

  @override
  Future<void> setAppCategory(String name, String category) async => appCategories[name] = category;

  @override
  Future<List<RiskDay>> riskHistory({int days = 7}) async => const [];
}

Future<void> pumpTime(WidgetTester t, FakeTimeApi api, {FakeUsage? usage}) async {
  t.view.physicalSize = const Size(1080, 2400);
  t.view.devicePixelRatio = 3;
  addTearDown(t.view.reset);
  SharedPreferences.setMockInitialValues({}); // the places of the apps are kept there
  await t.pumpWidget(MaterialApp(
    theme: buildTheme(Palette.light),
    home: Scaffold(body: TimeScreen(api: api, usage: usage ?? FakeUsage(), now: () => DateTime(2026, 10, 28, 12))),
  ));
  await t.pumpAndSettle();
}

void main() {
  test('the tab parses what the server sends', () {
    final o = TimeOverview.fromJson({
      'total_min': 120,
      'by_category': {'study': 120},
      'previous': null,
      'days': [
        {'date': '2026-10-26', 'by_category': {'study': 120}}
      ],
      'screen': {'study': 30},
      'screen_unknown_min': 5,
    });
    expect(o.totalMin, 120);
    expect(o.previous, isNull);
    expect(o.days.single.total, 120);
    expect(o.screenUnknownMin, 5);

    final a = ActivitiesData.fromJson({
      'weeks': 12,
      'activities': [
        {
          'key': 'sport',
          'name': 'Спорт',
          'goal_minutes': 240,
          'streak': 2,
          'active_days': 2,
          'goal_percent': 75,
          'total_min': 180,
          'heat': [
            {'date': 'x', 'minutes': 30},
            {'date': 'z', 'minutes': 0, 'active': true},
            {'date': 'y', 'minutes': 0, 'future': true},
          ],
        }
      ],
    });
    final act = a.activities.single;
    expect(act.goalPercent, 75);
    expect(act.heat.last.future, isTrue);
    expect(act.key, 'sport');
    expect(act.heat[1].active, isTrue);
  });

  test('period ranges: a calendar week, 30 and 90 days ending today', () {
    final today = DateTime(2026, 10, 28); // Wednesday
    expect(periodRange(TimePeriod.week, today), (DateTime(2026, 10, 26), DateTime(2026, 11, 2)));
    expect(periodRange(TimePeriod.week, today, weekShift: -1).$1, DateTime(2026, 10, 19));
    final (mf, mt) = periodRange(TimePeriod.month, today);
    expect(mt.difference(mf).inDays, 30);
    expect(mt, DateTime(2026, 10, 29));
    expect(periodRange(TimePeriod.quarter, today).$2.difference(periodRange(TimePeriod.quarter, today).$1).inDays, 90);
  });

  test('app categories map to the categories of the tab', () {
    expect(timeCategoryOf('study'), 'study');
    expect(timeCategoryOf('career'), 'work');
    expect(timeCategoryOf('leisure'), 'hobby');
    expect(timeCategoryOf('social'), 'other');
    expect(timeCategoryOf('unknown'), isNull);
    expect(appCategoryOf('study'), 'study');
    expect(timeCategoryOf(appCategoryOf('hobby')), 'hobby');
  });

  test('screen time: the server answer wins, the system category is the fallback, the rest has no category', () async {
    final day = DateTime(2026, 10, 27, 10);
    final usage = FakeUsage()
      ..list = [
        AppSession(pkg: 'com.editor', start: day, end: day.add(const Duration(minutes: 60)), androidCategory: -1),
        AppSession(pkg: 'com.game', start: day.add(const Duration(hours: 2)), end: day.add(const Duration(hours: 2, minutes: 30)), androidCategory: 0),
        AppSession(pkg: 'com.mystery', start: day.add(const Duration(hours: 4)), end: day.add(const Duration(hours: 4, minutes: 20)), androidCategory: -1),
      ];
    final api = FakeApi()..categories = {'com.editor': const NameCategory(category: 'study', confidence: 1, resolved: true)};
    final names = <String, NameCategory>{};
    final st = await loadScreenTime(usage: usage, api: api, from: DateTime(2026, 10, 26), to: DateTime(2026, 11, 2), names: names, compare: false);
    expect(st.minutesOf('study'), 60);
    expect(st.minutesOf('hobby'), 30); // from the system's category, the server did not know
    expect(st.unassigned.map((a) => a.pkg), ['com.mystery']);
    expect(st.unassignedMinutes, 20);
    // The second call asks about nothing it already knows.
    final calls = api.categorizeCalls.length;
    await loadScreenTime(usage: usage, api: api, from: DateTime(2026, 10, 26), to: DateTime(2026, 11, 2), names: names, compare: false);
    expect(api.categorizeCalls.length, calls);
  });

  testWidgets('the tab shows the categories and opens the detail of one', (t) async {
    await pumpTime(t, FakeTimeApi());
    expect(find.text('Где уходит время'), findsWidgets);
    expect(find.text('Учёба'), findsOneWidget);
    expect(find.text('Работа'), findsOneWidget);
    await t.tap(find.text('Учёба'));
    await t.pumpAndSettle();
    expect(find.text('На что ушло'), findsOneWidget);
    expect(find.text('Лекции и семинары'), findsOneWidget);
    expect(find.text('Записи'), findsOneWidget);
  });

  testWidgets('without usage access the apps card says so and the diagram stays', (t) async {
    await pumpTime(t, FakeTimeApi(), usage: FakeUsage()..access = false);
    expect(find.text('Нет доступа к статистике приложений'), findsOneWidget);
    expect(find.text('Учёба'), findsOneWidget);
  });

  testWidgets('activities follow the goals by themselves: no buttons to press', (t) async {
    await pumpTime(t, FakeTimeApi());
    await t.tap(find.text('Активности'));
    await t.pumpAndSettle();
    expect(find.text('Учёба'), findsOneWidget);
    expect(find.text('Карьера'), findsOneWidget);
    expect(find.text('62%'), findsOneWidget);
    expect(find.textContaining('Считается само'), findsOneWidget);
    // Nothing to start, mark or add by hand.
    expect(find.text('Начать'), findsNothing);
    expect(find.text('Заметка'), findsNothing);
    expect(find.text('Добавить активность'), findsNothing);
  });
}
