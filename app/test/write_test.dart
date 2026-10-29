import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/controller.dart';
import 'package:life_agent/main.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/settings.dart';
import 'package:life_agent/slot.dart';

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

Future<void> toVerdict(WidgetTester t, FakeApi api) async {
  await start(t, api);
  await t.enterText(find.byType(TextField).first, 'КВН в пятницу с 18 до 20');
  await t.pump();
  await t.tap(find.byTooltip('Разобрать'));
  await t.pumpAndSettle();
  await t.tap(find.text('Проверить и записать'));
  await t.pumpAndSettle();
}

Risks risks(String overload, {String noRest = 'green'}) =>
    Risks(sleep: risk('gray'), overload: risk(overload), noRest: risk(noRest), state: risk('gray'));

void main() {
  test('the request carries wall-clock times, the category flags and the rule', () {
    const s = SlotRequest(
        title: ' КВН ', start: Wall(2026, 10, 30, 18), end: Wall(2026, 10, 30, 20), category: 'hobby',
        recurrence: 'FREQ=WEEKLY;BYDAY=FR');
    expect(s.toJson(), {
      'title': 'КВН',
      'start': '2026-10-30T18:00',
      'end': '2026-10-30T20:00',
      'category': 'hobby',
      'category_confirmed': false,
      'confirmed': false,
      'recurrence': 'FREQ=WEEKLY;BYDAY=FR',
    });
  });

  test('a repeated payload keeps its key, a changed one gets a new key', () {
    final k = IdempotencyKeys();
    final a = k.forPayload('one');
    expect(k.forPayload('one'), a);
    expect(k.forPayload('two'), isNot(a));
    expect(a, hasLength(32));
  });

  testWidgets('free slot: verdict says yes, the event is written once and the phrase is cleared', (t) async {
    final api = FakeApi()..checkResult = sampleCheck(askCategory: false, source: 'memory', category: 'hobby');
    await toVerdict(t, api);

    expect(find.text('Можно добавить'), findsOneWidget);
    expect(api.checked.single.toJson()['start'], '2026-10-30T18:00');
    expect(api.checked.single.confirmed, isFalse);

    await t.tap(find.text('Записать в календарь'));
    await t.pumpAndSettle();

    expect(api.written, hasLength(1));
    expect(api.written.single.slot.confirmed, isFalse);
    expect(api.written.single.key, hasLength(32));
    expect(find.text('Записано в календарь'), findsOneWidget, reason: 'snackbar on the home screen');
    expect(t.widget<TextField>(find.byType(TextField)).controller!.text, isEmpty,
        reason: 'the phrase is cleared after a write');
  });

  testWidgets('a red risk needs an explicit "write anyway"', (t) async {
    final api = FakeApi()
      ..checkResult = sampleCheck(
        ok: false, needsConfirm: true, worsened: ['overload'], source: 'memory',
        reasons: ['Перегруз станет красным. учёба 11 ч в день'],
        before: risks('yellow'), after: risks('red'),
      );
    await toVerdict(t, api);

    expect(find.text('Лучше не добавлять'), findsOneWidget);
    expect(find.text('Перегруз станет красным. учёба 11 ч в день'), findsOneWidget);
    expect(find.text('Записать в календарь'), findsNothing);
    expect(find.text('Всё равно записать'), findsOneWidget);
    expect(api.written, isEmpty, reason: 'nothing is written before the tap');

    await t.tap(find.text('Всё равно записать'));
    await t.pumpAndSettle();
    expect(api.written.single.slot.confirmed, isTrue);
  });

  testWidgets('overlap is named in the reasons', (t) async {
    final api = FakeApi()
      ..checkResult = sampleCheck(
        ok: false, needsConfirm: true, source: 'memory',
        overlaps: [const Overlap(title: 'Пара', start: Wall(2026, 10, 30, 17), end: Wall(2026, 10, 30, 19))],
        reasons: ['Пересекается с «Пара» (17:00–19:00).'],
      );
    await toVerdict(t, api);
    expect(find.text('Пересекается с «Пара» (17:00–19:00).'), findsOneWidget);
  });

  testWidgets('an unknown category is asked first, the answer re-checks and unlocks writing', (t) async {
    final api = FakeApi()..checkResult = sampleCheck(askCategory: true, source: 'guess', category: 'hobby');
    await toVerdict(t, api);

    expect(find.text('Категория верная: «Хобби»?'), findsOneWidget);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Записать в календарь')).onPressed, isNull);

    api.checkResult = sampleCheck(askCategory: false, source: 'user', category: 'hobby');
    await t.tap(find.text('Да, «Хобби»'));
    await t.pumpAndSettle();

    expect(api.checked, hasLength(2));
    expect(api.checked.last.categoryConfirmed, isTrue);
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Записать в календарь')).onPressed, isNotNull);
  });

  testWidgets('picking another category re-checks with it', (t) async {
    final api = FakeApi()..checkResult = sampleCheck(askCategory: true, source: 'guess', category: 'hobby');
    await toVerdict(t, api);
    api.checkResult = sampleCheck(askCategory: false, category: 'study');
    await t.tap(find.widgetWithText(ChoiceChip, 'Учёба'));
    await t.pumpAndSettle();
    expect(api.checked.last.category, 'study');
    expect(api.checked.last.categoryConfirmed, isTrue);
  });

  testWidgets('a write error keeps the screen and the retry reuses the key', (t) async {
    final api = FakeApi()
      ..checkResult = sampleCheck(source: 'memory')
      ..writeError = const ApiException(ApiProblem.server, 'У сервера нет права записи в этот календарь.');
    await toVerdict(t, api);

    await t.tap(find.text('Записать в календарь'));
    await t.pumpAndSettle();
    expect(find.text('У сервера нет права записи в этот календарь.'), findsOneWidget);
    expect(find.text('Можно добавить'), findsOneWidget);

    api.writeError = null;
    await t.tap(find.text('Записать в календарь'));
    await t.pumpAndSettle();
    expect(api.written, hasLength(1));
  });

  testWidgets('the server may ask for confirmation at write time', (t) async {
    final api = FakeApi()
      ..checkResult = sampleCheck(source: 'memory')
      ..writeError = NeedsConfirm(sampleCheck(
          ok: false, needsConfirm: true, source: 'memory', reasons: ['Пересекается с «Пара» (18:00–19:00).']));
    await toVerdict(t, api);

    await t.tap(find.text('Записать в календарь'));
    await t.pumpAndSettle();
    expect(find.text('Пересекается с «Пара» (18:00–19:00).'), findsOneWidget);
    expect(find.text('Всё равно записать'), findsOneWidget);
  });

  testWidgets('check error offers a retry', (t) async {
    final api = FakeApi()..checkError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await toVerdict(t, api);
    expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);
    api.checkError = null;
    await t.tap(find.text('Повторить'));
    await t.pumpAndSettle();
    expect(find.text('Можно добавить'), findsOneWidget);
  });

  testWidgets('"Изменить" returns to the card without writing', (t) async {
    final api = FakeApi()..checkResult = sampleCheck(source: 'memory');
    await toVerdict(t, api);
    await t.tap(find.text('Изменить'));
    await t.pumpAndSettle();
    expect(find.text('Проверить и записать'), findsOneWidget);
    expect(api.written, isEmpty);
  });

  testWidgets('a task is saved right from the card', (t) async {
    final api = FakeApi()
      ..parseResult = sampleCard(
          kind: 'task', title: 'Курсовая', category: 'study', date: null, timeFrom: null, timeTo: null,
          durationMin: null, deadline: '2026-10-30', missing: ['duration_min']);
    await start(t, api);
    await t.enterText(find.byType(TextField).first, 'курсовая до пятницы');
    await t.pump();
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    await t.tap(find.widgetWithText(ChoiceChip, '6 ч'));
    await t.pump();
    await t.tap(find.text('Сохранить задачу'));
    await t.pumpAndSettle();

    expect(api.tasks, hasLength(1));
    expect(api.tasks.single.task.toJson(), {
      'title': 'Курсовая',
      'category': 'study',
      'category_confirmed': true,
      'duration_min': 360,
      'deadline': '2026-10-30',
    });
    expect(find.text('Задача сохранена'), findsOneWidget);
    expect(api.checked, isEmpty, reason: 'tasks are not checked against the calendar');
  });

  testWidgets('an incomplete card cannot be sent', (t) async {
    final api = FakeApi()..parseResult = sampleCard(timeFrom: null, timeTo: null, missing: ['time_from', 'time_to']);
    await start(t, api);
    await t.enterText(find.byType(TextField).first, 'КВН в пятницу');
    await t.pump();
    await t.tap(find.byTooltip('Разобрать'));
    await t.pumpAndSettle();
    expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Проверить и записать')).onPressed, isNull);
    expect(find.text('Заполни недостающие поля.'), findsOneWidget);
  });

  testWidgets('phone width with large text: the verdict does not overflow', (t) async {
    t.view.physicalSize = const Size(360, 780);
    t.view.devicePixelRatio = 1;
    t.platformDispatcher.textScaleFactorTestValue = 1.4;
    addTearDown(() {
      t.view.resetPhysicalSize();
      t.view.resetDevicePixelRatio();
      t.platformDispatcher.clearAllTestValues();
    });
    final api = FakeApi()
      ..checkResult = sampleCheck(
        ok: false, needsConfirm: true, askCategory: true, source: 'guess', worsened: ['overload'],
        reasons: ['Перегруз станет красным. учёба 11 ч в день (порог: жёлтый выше 6 ч, красный выше 9 ч)'],
        overlaps: [const Overlap(title: 'Экономика Экзамен', start: Wall(2026, 10, 30, 17), end: Wall(2026, 10, 30, 19))],
        before: risks('yellow'), after: risks('red', noRest: 'yellow'),
      );
    await toVerdict(t, api);
    expect(find.text('Лучше не добавлять'), findsOneWidget);
    expect(t.takeException(), isNull);
  });
}
