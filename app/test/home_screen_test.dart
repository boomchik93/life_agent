import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/controller.dart';
import 'package:life_agent/main.dart';
import 'package:life_agent/settings.dart';

import 'fakes.dart';

const conn = ConnectionSettings(baseUrl: 'https://x.example', token: 'tok');
final fixedNow = DateTime(2026, 10, 28, 12); // Wednesday of the sample week

Future<AppController> start(WidgetTester t, FakeApi? api, {MemoryStorage? storage}) async {
  final c = AppController(storage ?? MemoryStorage(connection: api == null ? const ConnectionSettings(baseUrl: '', token: '') : conn),
      apiBuilder: (s) => s.isComplete ? api : null);
  await c.load();
  await t.pumpWidget(MaterialApp(home: HomeRoot(controller: c, now: () => fixedNow)));
  await t.pumpAndSettle();
  return c;
}

void main() {
  testWidgets('shows four indicators with their state and the week by day', (t) async {
    final api = FakeApi(
      week: sampleWeek(
        sleep: risk('red', reasons: ['в среднем 5,0 ч сна']),
        overload: risk('yellow'),
        noRest: risk('green'),
        state: risk('gray'),
      ),
    );
    t.view.physicalSize = const Size(1080, 4000);
    t.view.devicePixelRatio = 3;
    addTearDown(t.view.reset);
    await start(t, api, storage: MemoryStorage(connection: conn, checkinDate: '2026-10-28'));

    for (final title in ['Недосып', 'Перегруз', 'Нет отдыха', 'Состояние']) {
      expect(find.text(title), findsOneWidget);
    }
    expect(find.text('Риск'), findsOneWidget);
    expect(find.text('Внимание'), findsOneWidget);
    expect(find.text('В норме'), findsOneWidget);
    expect(find.text('Нет данных'), findsOneWidget);

    expect(find.text('Понедельник, 26 октября'), findsOneWidget);
    expect(find.text('Пятница, 30 октября'), findsOneWidget);
    expect(find.text('18:00–20:00'), findsOneWidget);
    expect(find.text('КВН'), findsNWidgets(2)); // the "Дальше" card and the week list
    expect(find.text('Нет событий'), findsNWidgets(6));
  });

  testWidgets('tapping an indicator explains the reason', (t) async {
    final api = FakeApi(week: sampleWeek(sleep: risk('red', reasons: ['в среднем 5,0 ч сна'], metrics: {'avg_sleep_hours': 5})));
    await start(t, api, storage: MemoryStorage(connection: conn, checkinDate: '2026-10-28'));
    await t.tap(find.text('Недосып'));
    await t.pumpAndSettle();
    expect(find.text('в среднем 5,0 ч сна'), findsOneWidget);
    expect(find.text('Средний сон, ч'), findsOneWidget);
  });

  testWidgets('check-in card shows until today is done, then saving refreshes the indicators', (t) async {
    final api = FakeApi();
    final storage = MemoryStorage(connection: conn, checkinDate: '2026-10-27');
    await start(t, api, storage: storage);
    expect(find.text('Как ты сегодня?'), findsOneWidget);
    expect(api.calls.length, 1);

    await t.tap(find.text('Как ты сегодня?'));
    await t.pumpAndSettle();
    await t.tap(find.descendant(of: find.bySemanticsLabel('Энергия 2 из 5'), matching: find.text('2')));
    await t.tap(find.descendant(of: find.bySemanticsLabel('Настроение 2 из 5'), matching: find.text('2')));
    await t.pump();
    api.setWeek(sampleWeek(state: risk('red')));
    await t.tap(find.text('Сохранить'));
    await t.pumpAndSettle();

    expect(storage.checkinDate, '2026-10-28');
    expect(find.text('Как ты сегодня?'), findsNothing);
    expect(api.calls.length, 2, reason: 'week is reloaded after a check-in');
    expect(find.text('Риск'), findsOneWidget);
  });

  testWidgets('no settings: asks for them instead of calling the server', (t) async {
    await start(t, null);
    expect(find.text('Укажи адрес сервера и токен.'), findsOneWidget);
    await t.tap(find.widgetWithText(FilledButton, 'Настройки'));
    await t.pumpAndSettle();
    expect(find.text('Адрес сервера'), findsOneWidget);
  });

  testWidgets('network error shows a message and retry loads the week', (t) async {
    final api = FakeApi()..weekError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    await start(t, api, storage: MemoryStorage(connection: conn, checkinDate: '2026-10-28'));
    expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);

    api.weekError = null;
    await t.tap(find.text('Повторить'));
    await t.pumpAndSettle();
    expect(find.text('Недосып'), findsOneWidget);
  });

  testWidgets('wrong token offers settings', (t) async {
    final api = FakeApi()..weekError = const ApiException(ApiProblem.unauthorized, 'Токен не подошёл. Проверь токен в настройках.');
    await start(t, api);
    expect(find.text('Токен не подошёл. Проверь токен в настройках.'), findsOneWidget);
    expect(find.widgetWithText(TextButton, 'Настройки'), findsOneWidget);
  });

  testWidgets('partial calendar failure is visible', (t) async {
    final api = FakeApi(week: sampleWeek(warnings: ['ics: timeout']));
    await start(t, api, storage: MemoryStorage(connection: conn, checkinDate: '2026-10-28'));
    expect(find.textContaining('ics: timeout'), findsOneWidget);
  });

  testWidgets('week screen moves between weeks', (t) async {
    final api = FakeApi();
    await start(t, api, storage: MemoryStorage(connection: conn, checkinDate: '2026-10-28'));
    await t.tap(find.byTooltip('Неделя'));
    await t.pumpAndSettle();
    expect(find.text('26 октября – 1 ноября'), findsOneWidget);

    await t.tap(find.byTooltip('Следующая неделя'));
    await t.pumpAndSettle();
    expect(api.calls.last, 'week 2026-11-02');
    await t.tap(find.byTooltip('Прошлая неделя'));
    await t.pumpAndSettle();
    expect(api.calls.last, 'week 2026-10-26');
  });
}
