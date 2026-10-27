import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/controller.dart';
import 'package:life_agent/main.dart';
import 'package:life_agent/settings.dart';

import 'fakes.dart';

const conn = ConnectionSettings(baseUrl: 'https://x.example', token: 'tok');
final fixedNow = DateTime(2026, 10, 28, 12);

Future<void> start(WidgetTester t, FakeApi api) async {
  final c = AppController(MemoryStorage(connection: conn, checkinDate: '2026-10-28'),
      apiBuilder: (s) => s.isComplete ? api : null);
  await c.load();
  await t.pumpWidget(MaterialApp(
    locale: const Locale('ru'),
    supportedLocales: const [Locale('ru')],
    localizationsDelegates: GlobalMaterialLocalizations.delegates,
    home: HomeRoot(controller: c, now: () => fixedNow),
  ));
  await t.pumpAndSettle();
}

Future<void> type(WidgetTester t, String text) async {
  await t.enterText(find.byType(TextField).first, text);
  await t.pump();
}

void main() {
  testWidgets('send is off for an empty phrase', (t) async {
    await start(t, FakeApi());
    expect(t.widget<IconButton>(find.widgetWithIcon(IconButton, Icons.arrow_upward)).onPressed, isNull);
    await type(t, 'КВН в пятницу с 18 до 20');
    expect(t.widget<IconButton>(find.widgetWithIcon(IconButton, Icons.arrow_upward)).onPressed, isNotNull);
  });

  testWidgets('a phrase becomes an event card', (t) async {
    final api = FakeApi();
    await start(t, api);
    await type(t, '  КВН в пятницу с 18 до 20 ');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();

    expect(api.parsed, ['КВН в пятницу с 18 до 20']);
    expect(find.text('Событие'), findsWidgets);
    expect(find.text('КВН'), findsOneWidget);
    expect(find.text('Пятница, 30 октября'), findsOneWidget);
    expect(find.text('18:00'), findsOneWidget);
    expect(find.text('20:00'), findsOneWidget);
    expect(find.text('Длится 2 ч'), findsOneWidget);
    expect(find.textContaining('Не хватает'), findsNothing);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Проверить и записать')).onPressed, isNotNull);

    await t.tap(find.text('Назад к фразе'));
    await t.pumpAndSettle();
    expect(find.text('КВН в пятницу с 18 до 20'), findsOneWidget, reason: 'the phrase stays for a fix');
  });

  testWidgets('a task card shows duration and deadline', (t) async {
    final api = FakeApi()
      ..parseResult = sampleCard(
          kind: 'task', title: 'Курсовая', category: 'study', date: null, timeFrom: null, timeTo: null,
          durationMin: 360, deadline: '2026-10-30');
    await start(t, api);
    await type(t, 'курсовая 6 часов до пятницы');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();

    expect(find.text('Задача'), findsWidgets);
    expect(find.text('6 ч'), findsWidgets);
    expect(find.text('Пятница, 30 октября'), findsOneWidget);

    await t.tap(find.byTooltip('Меньше на 30 минут'));
    await t.pump();
    expect(find.text('5 ч 30 мин'), findsOneWidget);
  });

  testWidgets('missing fields are marked and filling them clears the mark', (t) async {
    final api = FakeApi()
      ..parseResult = sampleCard(
          kind: 'task', title: 'Реферат', category: 'study', date: null, timeFrom: null, timeTo: null,
          durationMin: null, deadline: null, missing: ['duration_min', 'deadline']);
    await start(t, api);
    await type(t, 'реферат');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();

    expect(find.text('Не хватает: длительность, срок. Заполни ниже.'), findsOneWidget);
    await t.tap(find.widgetWithText(ChoiceChip, '2 ч'));
    await t.tap(find.widgetWithText(ChoiceChip, 'Завтра'));
    await t.pump();
    expect(find.textContaining('Не хватает'), findsNothing);
    expect(find.text('Четверг, 29 октября'), findsOneWidget);
  });

  testWidgets('wrong end time is flagged', (t) async {
    await start(t, FakeApi());
    await type(t, 'КВН');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    await t.tap(find.text('20:00'));
    await t.pumpAndSettle();
    // The keyboard entry of the time picker is the simplest way to set 17:00 in a test.
    await t.tap(find.byIcon(Icons.keyboard_outlined));
    await t.pumpAndSettle();
    final fields = find.byType(TextField);
    await t.enterText(fields.at(0), '17');
    await t.enterText(fields.at(1), '00');
    await t.tap(find.text('ОК'));
    await t.pumpAndSettle();
    expect(find.text('Конец события не позже начала.'), findsOneWidget);
  });

  testWidgets('an error keeps the typed text and a retry works', (t) async {
    final api = FakeApi()..parseError = const ApiException(ApiProblem.llm, 'Модели не ответили.');
    await start(t, api);
    await type(t, 'КВН в пятницу');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();

    expect(find.textContaining('Модели не ответили.'), findsOneWidget);
    expect(find.text('Модели'), findsOneWidget, reason: 'the models are the problem, so settings are offered');
    expect(find.text('КВН в пятницу'), findsOneWidget);

    api.parseError = null;
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    expect(api.parsed, ['КВН в пятницу', 'КВН в пятницу']);
    expect(find.text('Назад к фразе'), findsOneWidget);
  });

  testWidgets('daily limit is shown with the reset time', (t) async {
    final api = FakeApi()
      ..parseError = const ApiException(ApiProblem.dailyLimit, 'Дневной лимит бесплатных моделей исчерпан. Сброс в 05:00.');
    await start(t, api);
    await type(t, 'КВН');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    expect(find.textContaining('Сброс в 05:00.'), findsOneWidget);
  });

  testWidgets('cancel drops a slow answer', (t) async {
    final api = FakeApi()..parseDelay = const Duration(seconds: 5);
    await start(t, api);
    await type(t, 'КВН');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pump();
    expect(find.textContaining('Разбираю фразу'), findsOneWidget);

    await t.tap(find.text('Отмена'));
    await t.pump();
    expect(find.textContaining('Разбираю фразу'), findsNothing);
    await t.pump(const Duration(seconds: 6));
    await t.pumpAndSettle();
    expect(find.text('Назад к фразе'), findsNothing, reason: 'the cancelled answer must not open a card');
  });

  testWidgets('phone width with large text: no overflow on home, card and models', (t) async {
    t.view.physicalSize = const Size(360, 780);
    t.view.devicePixelRatio = 1;
    t.platformDispatcher.textScaleFactorTestValue = 1.4;
    addTearDown(() {
      t.view.resetPhysicalSize();
      t.view.resetDevicePixelRatio();
      t.platformDispatcher.clearAllTestValues();
    });
    final api = FakeApi()
      ..parseResult = sampleCard(
          kind: 'task', title: 'Курсовая по базам данных', category: 'study', date: null, timeFrom: null,
          timeTo: null, durationMin: 360, deadline: '2026-10-30');
    await start(t, api);
    await type(t, 'курсовая 6 часов до пятницы');
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    expect(find.text('Назад к фразе'), findsOneWidget);
    await t.tap(find.text('Событие'));
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
  });
}
