import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/screens/checkin_screen.dart';

import 'fakes.dart';

Future<bool?> openScreen(WidgetTester t, FakeApi api) async {
  bool? result;
  await t.pumpWidget(MaterialApp(
    home: Builder(
      builder: (ctx) => Scaffold(
        body: TextButton(
          onPressed: () async =>
              result = await Navigator.of(ctx).push<bool>(MaterialPageRoute(builder: (_) => CheckinScreen(api: api))),
          child: const Text('open'),
        ),
      ),
    ),
  ));
  await t.tap(find.text('open'));
  await t.pumpAndSettle();
  return result;
}

Future<void> pick(WidgetTester t, String scale, int v) async {
  await t.tap(find.descendant(of: find.bySemanticsLabel('$scale $v из 5'), matching: find.text('$v')));
  await t.pump();
}

FilledButton saveButton(WidgetTester t) => t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Сохранить'));

void main() {
  test('parseSleepHours', () {
    expect(parseSleepHours(''), isNull);
    expect(parseSleepHours('  '), isNull);
    expect(parseSleepHours('7,5'), 7.5);
    expect(parseSleepHours('7.5'), 7.5);
    expect(parseSleepHours('0'), 0);
    expect(parseSleepHours('24'), 24);
    for (final bad in ['25', '-1', 'abc', '7 ч']) {
      expect(() => parseSleepHours(bad), throwsFormatException, reason: bad);
    }
  });

  testWidgets('save is disabled until both scales are chosen', (t) async {
    await openScreen(t, FakeApi());
    expect(saveButton(t).onPressed, isNull);
    await pick(t, 'Энергия', 4);
    expect(saveButton(t).onPressed, isNull);
    await pick(t, 'Настроение', 2);
    expect(saveButton(t).onPressed, isNotNull);
  });

  testWidgets('saves the answers and closes with true', (t) async {
    final api = FakeApi();
    bool? result;
    await t.pumpWidget(MaterialApp(
      home: Builder(
        builder: (ctx) => TextButton(
          onPressed: () async =>
              result = await Navigator.of(ctx).push<bool>(MaterialPageRoute(builder: (_) => CheckinScreen(api: api))),
          child: const Text('open'),
        ),
      ),
    ));
    await t.tap(find.text('open'));
    await t.pumpAndSettle();
    await pick(t, 'Энергия', 2);
    await pick(t, 'Настроение', 3);
    await t.enterText(find.byType(TextField), '6,5');
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.checkins.single, (energy: 2, mood: 3, sleep: 6.5));
    expect(result, isTrue);
    expect(find.byType(CheckinScreen), findsNothing);
  });

  testWidgets('sleep is optional', (t) async {
    final api = FakeApi();
    await openScreen(t, api);
    await pick(t, 'Энергия', 5);
    await pick(t, 'Настроение', 5);
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.checkins.single.sleep, isNull);
  });

  testWidgets('bad sleep hours are rejected before any request', (t) async {
    final api = FakeApi();
    await openScreen(t, api);
    await pick(t, 'Энергия', 3);
    await pick(t, 'Настроение', 3);
    await t.enterText(find.byType(TextField), '30');
    await t.tap(find.text('Сохранить'));
    await t.pump();
    expect(find.text('Часы сна: число от 0 до 24'), findsOneWidget);
    expect(api.checkins, isEmpty);
  });

  testWidgets('on a network error the answers stay and a retry works', (t) async {
    final api = FakeApi()..checkinError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await openScreen(t, api);
    await pick(t, 'Энергия', 1);
    await pick(t, 'Настроение', 4);
    await t.enterText(find.byType(TextField), '5');
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();

    expect(find.textContaining('Нет связи с сервером'), findsOneWidget);
    expect(find.byType(CheckinScreen), findsOneWidget, reason: 'screen must stay open');
    expect(find.text('5'), findsWidgets);
    expect(t.widget<TextField>(find.byType(TextField)).controller!.text, '5');
    expect(saveButton(t).onPressed, isNotNull, reason: 'choices are kept, retry is possible');

    api.checkinError = null;
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();
    expect(api.checkins.single, (energy: 1, mood: 4, sleep: 5.0));
    expect(find.byType(CheckinScreen), findsNothing);
  });
}
