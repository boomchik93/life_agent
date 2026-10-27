import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/plan.dart';
import 'package:life_agent/review.dart';
import 'package:life_agent/screens/review_screen.dart';

import 'fakes.dart';

Future<FakeApi> open(WidgetTester t, {FakeApi? api}) async {
  api ??= FakeApi();
  t.view.physicalSize = const Size(360 * 3, 900 * 3);
  t.view.devicePixelRatio = 3;
  addTearDown(t.view.reset);
  await t.pumpWidget(MaterialApp(
    locale: const Locale('ru'),
    supportedLocales: const [Locale('ru')],
    localizationsDelegates: GlobalMaterialLocalizations.delegates,
    home: ReviewScreen(api: api),
  ));
  await t.pumpAndSettle();
  return api;
}

Future<void> reveal(WidgetTester t, Finder f) async {
  await t.scrollUntilVisible(f, 200, scrollable: find.byType(Scrollable).first);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
}

void main() {
  test('the review answer is read, a missing norm stays missing', () {
    final r = Review.fromJson({
      'week_start': '2026-10-26',
      'week_end': '2026-11-01',
      'goals': [
        {'goal': 'study', 'title': 'Учёба', 'fact_min': 300, 'norm_min': 600, 'percent': 50},
        {'goal': 'career', 'title': 'Карьера', 'fact_min': 90, 'norm_min': null, 'percent': null},
      ],
      'leisure_min': 1, 'social_min': 2, 'sleep_min': 3, 'sleep_days': 1, 'unknown_percent': 4, 'night_min': 5,
      'contributions': 6,
      'gaps': [
        {'goal': 'study', 'title': 'Учёба', 'missing_min': 300}
      ],
      'balance': null,
      'warnings': ['w'],
      'text': ['t'],
    });
    expect(r.goals[0].percent, 50);
    expect(r.goals[1].normMin, isNull);
    expect(r.gaps.single.missingMin, 300);
    expect(r.balanceMin, isNull);
    expect(r.weekStart.ymd, '2026-10-26');
  });

  testWidgets('four equal goals with their facts and norms', (t) async {
    await open(t);
    for (final title in ['Учёба', 'Карьера', 'Спорт', 'Работа']) {
      expect(find.text(title), findsOneWidget);
    }
    expect(find.text('5 ч'), findsWidgets); // the study fact
    expect(find.text('50% от 10 ч'), findsOneWidget);
    expect(find.text('норма не задана'), findsNWidgets(2), reason: 'career and work have no norm');
    expect(find.byType(LinearProgressIndicator), findsNWidgets(2));
  });

  testWidgets('without norms the screen explains when a suggestion comes', (t) async {
    final api = FakeApi()..goalsState = sampleGoals(dataDays: 5);
    await open(t, api: api);
    expect(find.textContaining('Через 14 дней данных сервер предложит норму'), findsOneWidget);
  });

  testWidgets('a suggested norm is confirmed with one tap', (t) async {
    final api = FakeApi()..goalsState = sampleGoals(suggested: {'study': 660});
    await open(t, api: api);
    expect(find.textContaining('Учёба: предлагаю норму 11 ч в неделю'), findsOneWidget);
    await t.tap(find.text('Принять'));
    await t.pumpAndSettle();
    expect(api.goalAnswers.single, (goal: 'study', action: 'confirm', normMin: null));
    expect(find.textContaining('предлагаю норму'), findsNothing);
  });

  testWidgets('a suggestion can be refused: no norm', (t) async {
    final api = FakeApi()..goalsState = sampleGoals(suggested: {'sport': 180});
    await open(t, api: api);
    await t.tap(find.text('Без нормы'));
    await t.pumpAndSettle();
    expect(api.goalAnswers.single.action, 'clear');
  });

  testWidgets('a four-week recalculation can keep the old norm', (t) async {
    final api = FakeApi()..goalsState = sampleGoals(norms: {'study': 600}, suggested: {'study': 900});
    await open(t, api: api);
    expect(find.textContaining('пересмотр нормы. Было 10 ч в неделю'), findsOneWidget);
    await t.tap(find.text('Оставить как было'));
    await t.pumpAndSettle();
    expect(api.goalAnswers.single.action, 'keep');
  });

  testWidgets('tapping a goal sets its norm in hours', (t) async {
    final api = await open(t);
    await t.tap(find.text('Карьера'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '7,5');
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.goalAnswers.single, (goal: 'career', action: 'set', normMin: 450));
  });

  testWidgets('a wrong norm is refused in the dialog; 0 removes the norm', (t) async {
    final api = await open(t);
    await t.tap(find.text('Учёба'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextField), '500');
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(find.textContaining('от 0 до 168'), findsOneWidget);
    expect(api.goalAnswers, isEmpty);
    await t.enterText(find.byType(TextField), '0');
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.goalAnswers.single.action, 'clear');
  });

  testWidgets('gaps, the rest of the week and the unknown share', (t) async {
    final api = FakeApi()
      ..reviewResult = sampleReview(
        gaps: const [ReviewGap(title: 'Учёба', missingMin: 300), ReviewGap(title: 'Спорт', missingMin: 105)],
        warnings: const ['Неопознано 40% времени: больше 20%, обзор неполный.'],
        unknownPercent: 40,
      );
    await open(t, api: api);
    expect(find.text('Неопознано 40% времени: больше 20%, обзор неполный.'), findsOneWidget);
    await reveal(t, find.text('Больше всего не хватает'));
    expect(find.text('1 ч 45 мин'), findsOneWidget);
    await reveal(t, find.text('Остальная неделя'));
    expect(find.text('5 ч'), findsWidgets);
    expect(find.text('40%'), findsOneWidget);
    expect(find.text('13 ч 30 мин за 2 дн.'), findsOneWidget);
  });

  testWidgets('the week can be changed', (t) async {
    final api = await open(t);
    await t.tap(find.byTooltip('Прошлая неделя'));
    await t.pumpAndSettle();
    expect(api.reviewStarts, [null, '2026-10-19']);
  });

  testWidgets('the button plans the goals: preview first, the goals flag goes along', (t) async {
    final api = await open(t);
    await reveal(t, find.text('Разложить недостающее по неделе'));
    await t.tap(find.text('Разложить недостающее по неделе'));
    await t.pumpAndSettle();
    expect(find.text('Цели на неделю'), findsOneWidget);
    await t.tap(find.text('Собрать неделю'));
    await t.pumpAndSettle();
    expect(api.lastPlanWithGoals, isTrue);
    await t.tap(find.text('Применить'));
    await t.pumpAndSettle();
    await t.tap(find.text('Записать'));
    await t.pumpAndSettle();
    expect(api.applied.single.plan.applyJson()['goals'], isTrue);
  });

  testWidgets('goals left out because of a red risk are named', (t) async {
    final api = FakeApi()
      ..planResult = PlanData(
        from: const Wall(2026, 10, 26),
        to: const Wall(2026, 10, 26),
        placed: const [],
        unplaced: const [],
        deferred: const [],
        before: samplePlan().before,
        after: samplePlan().after,
        violations: const [],
        skippedGoals: const [SkippedGoal(title: 'Учёба', reason: 'Перегруз красный: учёбу и работу больше не добавляем.')],
      );
    await open(t, api: api);
    await reveal(t, find.text('Разложить недостающее по неделе'));
    await t.tap(find.text('Разложить недостающее по неделе'));
    await t.pumpAndSettle();
    await t.tap(find.text('Собрать неделю'));
    await t.pumpAndSettle();
    expect(find.textContaining('Учёба: Перегруз красный'), findsOneWidget);
    expect(find.textContaining('Все цели выполнены'), findsNothing);
  });

  testWidgets('a load error is readable and can be retried', (t) async {
    final api = FakeApi()..reviewError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await open(t, api: api);
    expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);
    api.reviewError = null;
    await t.tap(find.text('Повторить'));
    await t.pumpAndSettle();
    expect(find.text('Учёба'), findsOneWidget);
  });
}
