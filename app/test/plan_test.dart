import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/controller.dart';
import 'package:life_agent/format.dart';
import 'package:life_agent/main.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/plan.dart';
import 'package:life_agent/settings.dart';

import 'fakes.dart';

const conn = ConnectionSettings(baseUrl: 'https://x.example', token: 'tok');

Future<void> openPlan(WidgetTester t, FakeApi api) async {
  final c = AppController(MemoryStorage(connection: conn, checkinDate: '2026-10-28'),
      apiBuilder: (s) => s.isComplete ? api : null);
  await c.load();
  await t.pumpWidget(MaterialApp(
    locale: const Locale('ru'),
    supportedLocales: const [Locale('ru')],
    localizationsDelegates: GlobalMaterialLocalizations.delegates,
    home: HomeRoot(controller: c, now: () => DateTime(2026, 10, 28, 12)),
  ));
  await t.pumpAndSettle();
  await t.tap(find.byTooltip('План недели'));
  await t.pumpAndSettle();
}

Future<void> build(WidgetTester t) async {
  await t.tap(find.text('Собрать неделю'));
  await t.pumpAndSettle();
}

void main() {
  test('the apply body carries wall-clock times of the plan the user saw', () {
    expect(samplePlan().applyJson()['placed'], [
      {'item_id': 7, 'start': '2026-10-28T13:00', 'end': '2026-10-28T15:00'},
      {'item_id': 7, 'start': '2026-10-28T15:15', 'end': '2026-10-28T17:15'},
      {'item_id': 7, 'start': '2026-10-29T09:00', 'end': '2026-10-29T11:00'},
    ]);
  });

  test('the plan answer is read', () {
    Map<String, dynamic> risks(String color) => {
          for (final k in ['sleep', 'overload', 'no_rest', 'state']) k: {'color': color, 'reasons': [], 'metrics': {}},
        };
    final plan = PlanData.fromJson({
      'from': '2026-10-26T00:00:00+05:00',
      'to': '2026-11-02T00:00:00+05:00',
      'placed': [
        {
          'item_id': 1, 'title': 'Курсовая', 'category': 'study',
          'start': '2026-10-28T13:00:00+05:00', 'end': '2026-10-28T14:30:00+05:00',
        }
      ],
      'unplaced': [
        {'item_id': 2, 'title': 'Отчёт', 'missing_min': 150}
      ],
      'deferred': [
        {'item_id': 3, 'title': 'Диплом', 'deadline': '2026-11-20'}
      ],
      'before': risks('green'),
      'after': risks('gray'),
      'violations': [],
    });
    expect(plan.placed.single.minutes, 90);
    expect(plan.unplaced.single.missingMin, 150);
    expect(plan.deferred.single.deadline, '2026-11-20');
    expect(plan.after.sleep.color, RiskColor.gray);
  });

  test('minutes read as hours', () {
    expect(minutesLabel(45), '45 мин');
    expect(minutesLabel(180), '3 ч');
    expect(minutesLabel(150), '2 ч 30 мин');
  });

  testWidgets('build shows the plan by day, nothing is written before the confirmation', (t) async {
    final api = FakeApi();
    await openPlan(t, api);

    expect(find.text('Собрать неделю'), findsOneWidget);
    expect(api.plansBuilt, 0);
    await build(t);

    expect(api.plansBuilt, 1);
    expect(find.text('Среда, 28 октября'), findsOneWidget);
    expect(find.text('Четверг, 29 октября'), findsOneWidget);
    expect(find.text('13:00–15:00'), findsOneWidget);
    expect(find.text('Задач разложено на 6 ч, кусков: 3.'), findsOneWidget);
    expect(api.applied, isEmpty);
  });

  testWidgets('apply asks first; cancel writes nothing, confirm writes once', (t) async {
    final api = FakeApi();
    await openPlan(t, api);
    await build(t);

    await t.tap(find.text('Применить'));
    await t.pumpAndSettle();
    expect(find.text('Записать план в календарь?'), findsOneWidget);
    await t.tap(find.text('Отмена'));
    await t.pumpAndSettle();
    expect(api.applied, isEmpty);

    await t.tap(find.text('Применить'));
    await t.pumpAndSettle();
    await t.tap(find.text('Записать'));
    await t.pumpAndSettle();
    expect(api.applied, hasLength(1));
    expect(api.applied.single.key, hasLength(32));
    expect(find.text('Записано в календарь: 3'), findsOneWidget);
    expect(find.text('Применить'), findsNothing, reason: 'no second write after success');
  });

  testWidgets('a failed write keeps the plan on screen and a retry still works', (t) async {
    final api = FakeApi()..applyError = const ApiException(ApiProblem.server, 'Календарь принял не всё.');
    await openPlan(t, api);
    await build(t);
    await t.tap(find.text('Применить'));
    await t.pumpAndSettle();
    await t.tap(find.text('Записать'));
    await t.pumpAndSettle();
    expect(find.text('Календарь принял не всё.'), findsOneWidget);
    expect(find.text('13:00–15:00'), findsOneWidget);

    api.applyError = null;
    await t.tap(find.text('Применить'));
    await t.pumpAndSettle();
    await t.tap(find.text('Записать'));
    await t.pumpAndSettle();
    expect(api.applied, hasLength(1));
    expect(find.text('Записано в календарь: 3'), findsOneWidget);
  });

  testWidgets('tasks that did not fit show the missing time', (t) async {
    final api = FakeApi()
      ..planResult = samplePlan(
        unplaced: const [PlanUnplaced(itemId: 8, title: 'Отчёт', missingMin: 150)],
        deferred: const [PlanDeferred(itemId: 9, title: 'Диплом', deadline: '2026-11-20')],
      );
    await openPlan(t, api);
    await build(t);
    await t.scrollUntilVisible(find.text('Диплом'), 200, scrollable: find.byType(Scrollable).first);
    expect(find.textContaining('Не хватает 2 ч 30 мин'), findsOneWidget);
    expect(find.text('Диплом'), findsOneWidget);
  });

  testWidgets('a plan with violations cannot be applied', (t) async {
    final api = FakeApi()..planResult = samplePlan(violations: ['учёба подряд 3.5 ч']);
    await openPlan(t, api);
    await build(t);
    expect(find.textContaining('учёба подряд 3.5 ч'), findsOneWidget);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Применить')).onPressed, isNull);
  });

  testWidgets('no tasks: the screen says how to add one', (t) async {
    final api = FakeApi()..planResult = samplePlan(placed: const []);
    await openPlan(t, api);
    await build(t);
    expect(find.textContaining('Открытых задач с дедлайном нет'), findsOneWidget);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Применить')).onPressed, isNull);
  });

  testWidgets('a server error is readable and the build can be repeated', (t) async {
    final api = FakeApi()..planError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await openPlan(t, api);
    await build(t);
    expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);

    api.planError = null;
    await t.tap(find.text('Повторить'));
    await t.pumpAndSettle();
    expect(find.text('13:00–15:00'), findsOneWidget);
  });

  testWidgets('phone width with large text: no overflow', (t) async {
    t.view.physicalSize = const Size(360 * 3, 740 * 3);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    final api = FakeApi()
      ..planResult = samplePlan(
        unplaced: const [
          PlanUnplaced(itemId: 8, title: 'Очень длинное название задачи для проверки переноса строк', missingMin: 150)
        ],
      );
    await openPlan(t, api);
    await build(t);
    expect(t.takeException(), isNull);
  });
}
