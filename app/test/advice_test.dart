import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/advice.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/background.dart';
import 'package:life_agent/controller.dart';
import 'package:life_agent/main.dart';
import 'package:life_agent/notifications.dart';
import 'package:life_agent/screens/advice_screen.dart';
import 'package:life_agent/settings.dart';

import 'fakes.dart';
import 'health_test.dart' show FakeScheduler, FakeSource;

class FakeNotifier implements Notifier {
  final shown = <({int id, String title, String body})>[];
  bool allowed = true;
  int asked = 0;

  @override
  Future<void> show({required int id, required String title, required String body}) async =>
      shown.add((id: id, title: title, body: body));

  @override
  Future<bool> requestPermission() async {
    asked++;
    return allowed;
  }
}

Widget app(Widget home) => MaterialApp(
      locale: const Locale('ru'),
      supportedLocales: const [Locale('ru')],
      localizationsDelegates: GlobalMaterialLocalizations.delegates,
      home: home,
    );

void main() {
  test('phrases that ask for an advice', () {
    for (final yes in ['совет', 'Дай совет', 'дай мне совет!', 'нужен совет', 'хочу новый совет', ' Совет по выгоранию ']) {
      expect(isAdviceRequest(yes), isTrue, reason: yes);
    }
    for (final no in ['КВН в пятницу с 18 до 20', 'совет директоров в пятницу в 15:00', 'курсовая 6 часов до пятницы', '']) {
      expect(isAdviceRequest(no), isFalse, reason: no);
    }
  });

  test('an advice reads and explains its reasons in words', () {
    final a = Advice.fromJson({
      'id': 3,
      'at': '2026-10-28T12:00:00+05:00',
      'reason': 'overload,sleep',
      'text': 'Ляг пораньше.',
      'source': 'auto',
    });
    expect(a.reasonText, 'нагрузка выше нормы, недосып');
    expect(a.asked, isFalse);
    expect(Advice.fromJson({'id': 1, 'at': '2026-10-28T12:00:00+05:00', 'reason': 'asked', 'text': 'x', 'source': 'user'}).asked, isTrue);
  });

  group('AdviceScreen', () {
    late FakeApi api;
    late MemoryStorage storage;
    late FakeScheduler sched;
    late FakeNotifier notifier;

    Future<void> open(WidgetTester t, {bool askNow = false}) async {
      api = FakeApi();
      storage = MemoryStorage();
      sched = FakeScheduler();
      notifier = FakeNotifier();
      await t.pumpWidget(app(AdviceScreen(api: api, storage: storage, scheduler: sched, notifier: notifier, askNow: askNow)));
      await t.pumpAndSettle();
    }

    testWidgets('empty history, then an asked advice appears on top', (t) async {
      await open(t);
      expect(find.text('Советов пока не было.'), findsOneWidget);
      await t.tap(find.text('Попросить совет'));
      await t.pumpAndSettle();
      expect(api.adviceRequests, 1);
      expect(find.text('Сдвинь сдачу на среду. Ляг до полуночи.'), findsOneWidget);
      expect(find.textContaining('По запросу'), findsOneWidget);
    });

    testWidgets('an error is shown and the history stays', (t) async {
      await open(t);
      api.history = [sampleAdvice(id: 5, text: 'Старый совет.', source: 'auto', reason: 'overload')];
      api.adviceError = const ApiException(ApiProblem.dailyLimit, 'Дневной лимит бесплатных моделей исчерпан. Сброс в 05:00.');
      await t.tap(find.text('Попросить совет'));
      await t.pumpAndSettle();
      expect(find.textContaining('Сброс в 05:00'), findsOneWidget);
      expect(find.text('Попросить совет'), findsOneWidget, reason: 'the button is back for another try');
    });

    testWidgets('the phrase "дай совет" asks at once', (t) async {
      await open(t, askNow: true);
      expect(api.adviceRequests, 1);
      expect(find.text('Сдвинь сдачу на среду. Ляг до полуночи.'), findsOneWidget);
    });

    testWidgets('old advice is listed with its reason', (t) async {
      api = FakeApi()..history = [sampleAdvice(id: 5, text: 'Старый совет.', source: 'auto', reason: 'overload,state')];
      await t.pumpWidget(app(AdviceScreen(
          api: api, storage: MemoryStorage(), scheduler: FakeScheduler(), notifier: FakeNotifier())));
      await t.pumpAndSettle();
      expect(find.text('Старый совет.'), findsOneWidget);
      expect(find.textContaining('Сам: нагрузка выше нормы, плохое состояние'), findsOneWidget);
    });

    testWidgets('notifications: permission, flag and the background task', (t) async {
      await open(t);
      await t.tap(find.text('Уведомления о советах'));
      await t.pumpAndSettle();
      expect(notifier.asked, 1);
      expect(storage.adviceOn, isTrue);
      expect(sched.starts, 1);

      await t.tap(find.text('Уведомления о советах'));
      await t.pumpAndSettle();
      expect(storage.adviceOn, isFalse);
      expect(sched.stops, 1);
    });

    testWidgets('a refused permission leaves notifications off', (t) async {
      await open(t);
      notifier.allowed = false;
      await t.tap(find.text('Уведомления о советах'));
      await t.pumpAndSettle();
      expect(storage.adviceOn, isFalse);
      expect(sched.starts, 0);
      expect(find.textContaining('Уведомления запрещены'), findsOneWidget);
    });

    testWidgets('the health sync keeps the task alive when notifications go off', (t) async {
      await open(t);
      storage.healthOn = true;
      await t.tap(find.text('Уведомления о советах'));
      await t.pumpAndSettle();
      await t.tap(find.text('Уведомления о советах'));
      await t.pumpAndSettle();
      expect(sched.stops, 0);
      expect(sched.starts, 2);
    });

    testWidgets('private data: a warning first, nothing saved on refusal', (t) async {
      await open(t);
      await t.tap(find.text('Отправлять в совет сон и чек-ин'));
      await t.pumpAndSettle();
      expect(find.textContaining('могут сохранять запросы'), findsOneWidget);
      await t.tap(find.text('Не отправлять'));
      await t.pumpAndSettle();
      expect(api.adviceSaved, isEmpty);

      await t.tap(find.text('Отправлять в совет сон и чек-ин'));
      await t.pumpAndSettle();
      await t.tap(find.text('Отправлять'));
      await t.pumpAndSettle();
      expect(api.adviceSaved.single.includePrivate, isTrue);

      await t.tap(find.text('Отправлять в совет сон и чек-ин'));
      await t.pumpAndSettle();
      expect(api.adviceSaved.last.includePrivate, isFalse, reason: 'switching off needs no warning');
    });
  });

  group('background jobs', () {
    test('the prepared advice becomes a notification once', () async {
      final api = FakeApi()..pending = sampleAdvice(id: 9, text: 'Выбери вечер без дел.');
      final n = FakeNotifier();
      final storage = MemoryStorage()..adviceOn = true;
      expect(await runBackgroundJobs(api, storage, FakeSource(), n), isTrue);
      expect(n.shown.single.body, 'Выбери вечер без дел.');
      expect(n.shown.single.id, 9);
      await runBackgroundJobs(api, storage, FakeSource(), n);
      expect(n.shown, hasLength(1), reason: 'the server handed it over once');
    });

    test('with notifications off nothing is fetched', () async {
      final api = FakeApi()..pending = sampleAdvice();
      final n = FakeNotifier();
      await runBackgroundJobs(api, MemoryStorage(), FakeSource(), n);
      expect(n.shown, isEmpty);
      expect(api.pending, isNotNull);
    });

    test('health and advice both run; a network error asks for a retry', () async {
      final api = FakeApi()
        ..healthError = const ApiException(ApiProblem.network, 'Нет связи')
        ..pending = sampleAdvice();
      final n = FakeNotifier();
      final storage = MemoryStorage()
        ..healthOn = true
        ..adviceOn = true;
      final src = FakeSource()..samples = [];
      final ok = await runBackgroundJobs(api, storage, src, n, now: () => DateTime(2026, 10, 28, 13));
      // No readings: nothing was sent, so no network error. The advice still shows.
      expect(ok, isTrue);
      expect(n.shown, hasLength(1));
      expect(storage.healthAt, startsWith('2026-10-28T13:00'));

      api.pendingError = const ApiException(ApiProblem.network, 'Нет связи');
      expect(await runBackgroundJobs(api, storage, src, n), isFalse);
      api.pendingError = const ApiException(ApiProblem.server, 'Сервер сломался');
      expect(await runBackgroundJobs(api, storage, src, n), isTrue, reason: 'a server error is not retried at once');
    });
  });

  group('home screen', () {
    Future<(FakeApi, FakeNotifier)> start(WidgetTester t) async {
      final api = FakeApi();
      final n = FakeNotifier();
      final c = AppController(
          MemoryStorage(connection: const ConnectionSettings(baseUrl: 'https://x.example', token: 'tok'), checkinDate: '2026-10-28'),
          apiBuilder: (s) => s.isComplete ? api : null);
      await c.load();
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: HomeRoot(controller: c, now: () => DateTime(2026, 10, 28, 12), scheduler: FakeScheduler(), notifier: n),
      ));
      await t.pumpAndSettle();
      return (api, n);
    }

    testWidgets('the typed phrase "дай совет" opens the advice and asks, without parsing', (t) async {
      final (api, _) = await start(t);
      await t.enterText(find.byType(TextField).first, 'дай совет');
      await t.pump();
      await t.tap(find.byTooltip('Разобрать'));
      await t.pumpAndSettle();
      expect(api.parsed, isEmpty, reason: 'no model call is spent on an advice request');
      expect(api.adviceRequests, 1);
      expect(find.text('Сдвинь сдачу на среду. Ляг до полуночи.'), findsOneWidget);
    });

    testWidgets('the advice opens from the "Ещё" tab', (t) async {
      final (api, _) = await start(t);
      await t.tap(find.text('Ещё'));
      await t.pumpAndSettle();
      await t.tap(find.text('Совет'));
      await t.pumpAndSettle();
      expect(find.text('Попросить совет'), findsOneWidget);
      expect(api.adviceRequests, 0);
    });
  });
}
