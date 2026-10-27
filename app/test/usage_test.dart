import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/background.dart';
import 'package:life_agent/screens/sources_screen.dart';
import 'package:life_agent/usage.dart';
import 'package:life_usage/life_usage.dart';

import 'fakes.dart';
import 'advice_test.dart' show FakeNotifier;
import 'health_test.dart' show FakeScheduler, FakeSource;

final day = DateTime(2026, 10, 28);
DateTime t(int h, [int m = 0]) => DateTime(2026, 10, 28, h, m);

AppSession s(String pkg, DateTime a, DateTime b, [int cat = -1]) =>
    AppSession(pkg: pkg, start: a, end: b, androidCategory: cat);

/// A server that knows what the test tells it.
class Server {
  Server(this.known);

  final Map<String, NameCategory> known;
  final calls = <({List<String> names, bool llm})>[];

  Future<Map<String, NameCategory>> call(List<String> names, {required bool llm}) async {
    calls.add((names: names, llm: llm));
    return {
      for (final n in names) n: known[n] ?? const NameCategory(category: 'unknown', confidence: 0, resolved: false),
    };
  }
}

/// Scrolls the long sources list until the finder is fully on screen.
Future<void> reveal(WidgetTester t, Finder f) async {
  await t.scrollUntilVisible(f, 200, scrollable: find.byType(Scrollable).first);
  await t.ensureVisible(f);
  await t.pumpAndSettle();
}

Map<String, int> minutes(UsageDay d) => {for (final r in d.rows) r.category: r.minutes};

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

void main() {
  group('summarizeUsage', () {
    test('the system category is enough: no name leaves the phone', () async {
      final srv = Server({});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.game', t(20), t(21), 0), s('com.chat', t(21), t(21, 30), 4)],
        events: const [],
        categorize: srv.call,
      );
      expect(minutes(d), {'leisure': 60, 'social': 30});
      expect(srv.calls, isEmpty);
    });

    test('an app the system cannot place goes to the dictionary first, the model is not asked', () async {
      final srv = Server({'com.ide': const NameCategory(category: 'career', confidence: 1, resolved: true)});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.ide', t(10), t(12), 7)],
        events: const [],
        categorize: srv.call,
      );
      expect(minutes(d), {'career': 120});
      expect(srv.calls.map((c) => '${c.names.join(",")}:${c.llm}'), ['com.ide:false']);
    });

    test('productivity is not sure enough by itself', () {
      expect(androidMetaGuess(7)!.confidence, lessThan(confidenceThreshold));
      expect(androidMetaGuess(0)!.confidence, greaterThanOrEqualTo(confidenceThreshold));
      expect(androidMetaGuess(-1), isNull);
      expect(androidMetaGuess(6), isNull);
    });

    test('calendar context: time inside a study event is study, the rest goes to the model', () async {
      final srv = Server({'com.browser': const NameCategory(category: 'leisure', confidence: 0.8, resolved: true)});
      // The browser is not in the dictionary: the first call (no model) leaves it unresolved.
      final first = Server({});
      var asked = <bool>[];
      Future<Map<String, NameCategory>> categorize(List<String> names, {required bool llm}) {
        asked.add(llm);
        return (llm ? srv : first).call(names, llm: llm);
      }

      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.browser', t(9), t(11))],
        events: [ContextEvent(t(10), t(12), 'study')],
        categorize: categorize,
      );
      expect(minutes(d), {'study': 60, 'leisure': 60});
      expect(asked, [false, true]);
      expect(srv.calls.single.names, ['com.browser']);
    });

    test('if all the time is inside an event, the model is not asked at all', () async {
      final srv = Server({});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.browser', t(10, 15), t(11, 45))],
        events: [ContextEvent(t(10), t(12), 'work')],
        categorize: srv.call,
      );
      expect(minutes(d), {'work': 90});
      expect(srv.calls.map((c) => c.llm), [false]);
    });

    test('a name already answered "unknown" is not asked again', () async {
      final srv = Server({'com.odd': const NameCategory(category: 'unknown', confidence: 0.3, resolved: true)});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.odd', t(14), t(15))],
        events: const [],
        categorize: srv.call,
      );
      expect(minutes(d), {'unknown': 60});
      expect(srv.calls.map((c) => c.llm), [false]);
    });

    test('a low-confidence model answer stays unknown', () async {
      final srv = Server({'com.vague': const NameCategory(category: 'sport', confidence: 0.4, resolved: true)});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.vague', t(14), t(15))],
        events: const [],
        categorize: srv.call,
      );
      expect(minutes(d), {'unknown': 60});
    });

    test('night minutes count the time in the sleep window; days are cut at midnight', () async {
      final srv = Server({});
      final d = await summarizeUsage(
        day: day,
        sessions: [
          s('com.game', DateTime(2026, 10, 27, 23, 30), t(1, 30), 0), // 1.5 h of this day's morning
          s('com.game', t(6, 30), t(8), 0), // half an hour before 07:00
          s('com.game', t(22, 30), DateTime(2026, 10, 29, 0, 30), 0), // 1.5 h to midnight, an hour of it at night
        ],
        events: const [],
        categorize: srv.call,
      );
      final leisure = d.rows.single;
      expect(leisure.minutes, 90 + 90 + 90);
      expect(leisure.nightMinutes, 90 + 30 + 60);
    });

    test('the confidence of a row is the minutes-weighted mean', () async {
      final srv = Server({});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.a', t(10), t(11), 0), s('com.b', t(12), t(13), 5)], // 0.9 and 0.75
        events: const [],
        categorize: srv.call,
      );
      expect(d.rows.single.category, 'leisure');
      expect(d.rows.single.confidence, closeTo(0.825, 0.01)); // (60 * 0.9 + 60 * 0.75) / 120
    });

    test('the summary holds categories and numbers only, never a package name', () async {
      final srv = Server({'com.secret.app': const NameCategory(category: 'career', confidence: 1, resolved: true)});
      final d = await summarizeUsage(
        day: day,
        sessions: [s('com.secret.app', t(10), t(11), 7), s('com.other.thing', t(12), t(13))],
        events: const [],
        categorize: srv.call,
      );
      final body = jsonEncode(d.toJson('android_usage'));
      expect(body, isNot(contains('com.')));
      expect(body, isNot(contains('secret')));
      expect(d.toJson('android_usage').keys, ['source', 'date', 'rows']);
      expect((d.toJson('android_usage')['rows'] as List).first.keys, ['category', 'minutes', 'night_minutes', 'confidence']);
      expect(d.date, '2026-10-28');
    });

    test('no sessions, no rows; a zero-length session is ignored', () async {
      final srv = Server({});
      final d = await summarizeUsage(day: day, sessions: [s('com.game', t(10), t(10), 0)], events: const [], categorize: srv.call);
      expect(d.rows, isEmpty);
    });
  });

  group('UsageSync', () {
    final now = DateTime(2026, 10, 28, 13);

    test('without access nothing is read or sent', () async {
      final api = FakeApi();
      final src = FakeUsage()..access = false;
      final r = await UsageSync(api: api, source: src, now: () => now).run();
      expect(r.sent, 0);
      expect(r.skipped, contains('доступа'));
      expect(api.activitySent, isEmpty);
    });

    test('sends yesterday and today as android_usage and reports the unknown share', () async {
      final api = FakeApi();
      final src = FakeUsage()
        ..list = [
          for (final d in [27, 28]) ...[
            s('com.game', DateTime(2026, 10, d, 9), DateTime(2026, 10, d, 10), 0),
            s('com.mystery', DateTime(2026, 10, d, 11), DateTime(2026, 10, d, 11, 30)),
          ],
        ];
      final r = await UsageSync(api: api, source: src, now: () => now).run();
      expect(api.activitySent.map((e) => e.source).toSet(), {'android_usage'});
      expect(r.sent, 2);
      expect(r.unknownPercent, 33); // 30 of 90 minutes, on both days
    });

    test('a calendar failure does not stop the summary', () async {
      final api = FakeApi()..weekError = const ApiException(ApiProblem.network, 'x');
      final src = FakeUsage()..list = [s('com.game', t(9), t(10), 0)];
      final r = await UsageSync(api: api, source: src, now: () => now).run();
      expect(r.sent, greaterThan(0));
    });

    test('an empty period is explained', () async {
      final r = await UsageSync(api: FakeApi(), source: FakeUsage(), now: () => now).run();
      expect(r.sent, 0);
      expect(r.skipped, contains('нет данных'));
    });
  });

  group('background', () {
    final now = DateTime(2026, 10, 28, 13);

    test('the usage sums are sent only when the source is on', () async {
      final api = FakeApi();
      final src = FakeUsage()..list = [s('com.game', t(9), t(10), 0)];
      final storage = MemoryStorage();
      await runBackgroundJobs(api, storage, FakeSource(), FakeNotifier(), usage: src, now: () => now);
      expect(api.activitySent, isEmpty);

      storage.usageOn = true;
      expect(await runBackgroundJobs(api, storage, FakeSource(), FakeNotifier(), usage: src, now: () => now), isTrue);
      expect(api.activitySent.single.source, 'android_usage');
      expect(api.activitySent.single.day.rows.single.category, 'leisure');
    });

    test('a network error asks for a retry, a switched-off source does not', () async {
      final src = FakeUsage()..list = [s('com.game', t(9), t(10), 0)];
      final storage = MemoryStorage()..usageOn = true;
      final api = FakeApi()..activityError = const ApiException(ApiProblem.network, 'x');
      expect(await runBackgroundJobs(api, storage, FakeSource(), FakeNotifier(), usage: src, now: () => now), isFalse);
      api.activityError = const ApiException(ApiProblem.server, 'Этот источник выключен.');
      expect(await runBackgroundJobs(api, storage, FakeSource(), FakeNotifier(), usage: src, now: () => now), isTrue);
    });

    test('the task stays alive while any job needs it', () async {
      final storage = MemoryStorage();
      final sched = FakeScheduler();
      await applyBackground(storage, sched);
      expect(sched.stops, 1);
      storage.usageOn = true;
      await applyBackground(storage, sched);
      expect(sched.starts, 1);
    });
  });

  group('SourcesScreen', () {
    late FakeApi api;
    late MemoryStorage storage;
    late FakeUsage usage;
    late FakeScheduler sched;
    final clipboard = <String>[];

    Future<void> open(WidgetTester t) async {
      api = FakeApi();
      storage = MemoryStorage();
      usage = FakeUsage();
      sched = FakeScheduler();
      clipboard.clear();
      t.view.physicalSize = const Size(360 * 3, 900 * 3);
      t.view.devicePixelRatio = 3;
      addTearDown(t.view.reset);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform,
          (call) async {
        if (call.method == 'Clipboard.setData') clipboard.add((call.arguments as Map)['text'] as String);
        return null;
      });
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: SourcesScreen(
            api: api, storage: storage, usage: usage, scheduler: sched, now: () => DateTime(2026, 10, 28, 13)),
      ));
      await t.pumpAndSettle();
    }

    testWidgets('lists all the sources with their switches', (t) async {
      await open(t);
      for (final title in ['Телефон: время в приложениях', 'Mac: ActivityWatch', 'Steam']) {
        expect(find.text(title), findsOneWidget);
      }
      expect(find.text('Данных пока нет'), findsWidgets);
    });

    testWidgets('a server source switches on the server', (t) async {
      await open(t);
      await t.tap(find.text('Steam'));
      await t.pumpAndSettle();
      expect(api.sourceList.firstWhere((x) => x.source == 'steam').enabled, isFalse);
    });

    testWidgets('the phone source needs the access first', (t) async {
      await open(t);
      usage.access = false;
      await t.tap(find.text('Телефон: время в приложениях'));
      await t.pumpAndSettle();
      expect(find.textContaining('Сначала выдай доступ'), findsOneWidget);
      expect(storage.usageOn, isFalse);
      expect(sched.starts, 0);
    });

    testWidgets('the phone source with access starts the background task', (t) async {
      await open(t);
      await t.tap(find.text('Телефон: время в приложениях'));
      await t.pumpAndSettle();
      expect(storage.usageOn, isTrue);
      expect(sched.starts, 1);
      await t.tap(find.text('Телефон: время в приложениях'));
      await t.pumpAndSettle();
      expect(storage.usageOn, isFalse);
      expect(sched.stops, 1);
    });

    testWidgets('with the access granted the sums can be sent at once', (t) async {
      await open(t);
      await reveal(t, find.text('Отправить сейчас'));
      expect(find.text('Доступ к истории использования выдан.'), findsOneWidget);
      expect(find.text('Отправить сейчас'), findsOneWidget);
    });

    testWidgets('no access: the button opens the system screen', (t) async {
      api = FakeApi();
      usage = FakeUsage()..access = false;
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: SourcesScreen(api: api, storage: MemoryStorage(), usage: usage, scheduler: FakeScheduler()),
      ));
      await t.pumpAndSettle();
      await reveal(t, find.text('Открыть настройки доступа'));
      expect(find.textContaining('выдай доступ вручную'), findsOneWidget);
      await t.tap(find.text('Открыть настройки доступа'));
      expect(usage.opened, 1);
    });

    testWidgets('deleting a source asks first', (t) async {
      await open(t);
      await t.tap(find.text('Удалить данные источника').first);
      await t.pumpAndSettle();
      expect(find.text('Удалить данные источника?'), findsOneWidget);
      await t.tap(find.text('Отмена'));
      await t.pumpAndSettle();
      expect(api.deletedSources, isEmpty);

      await t.tap(find.text('Удалить данные источника').first);
      await t.pumpAndSettle();
      await t.tap(find.text('Удалить'));
      await t.pumpAndSettle();
      expect(api.deletedSources, ['android_usage']);
      expect(find.text('Удалено строк: 7.'), findsOneWidget);
    });

    testWidgets('delete everything needs the word typed', (t) async {
      await open(t);
      await reveal(t, find.text('Удалить всё'));
      await t.tap(find.text('Удалить всё'));
      await t.pumpAndSettle();
      expect(t.widget<FilledButton>(find.widgetWithText(FilledButton, 'Удалить всё')).onPressed, isNull);
      await t.enterText(find.descendant(of: find.byType(AlertDialog), matching: find.byType(TextField)), 'удалить');
      await t.pump();
      await t.tap(find.widgetWithText(FilledButton, 'Удалить всё'));
      await t.pumpAndSettle();
      expect(api.allDeleted, isTrue);
    });

    testWidgets('export is copied', (t) async {
      await open(t);
      await reveal(t, find.text('Экспорт всех данных'));
      await t.tap(find.text('Экспорт всех данных'));
      await t.pumpAndSettle();
      expect(clipboard.single, '{"tables":{}}');
      expect(find.textContaining('Экспорт скопирован'), findsOneWidget);
    });

    testWidgets('the city for the weather is shown and saved', (t) async {
      await open(t);
      api.city = 'Пермь';
      await t.pumpWidget(const SizedBox());
      api = FakeApi()..city = 'Пермь';
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: SourcesScreen(api: api, storage: MemoryStorage(), usage: FakeUsage(), scheduler: FakeScheduler()),
      ));
      await t.pumpAndSettle();
      await reveal(t, find.text('Город для погоды'));
      expect(find.widgetWithText(TextField, 'Пермь'), findsOneWidget);
      await t.enterText(find.widgetWithText(TextField, 'Пермь'), '  Екатеринбург ');
      await t.tap(find.text('Сохранить'));
      await t.pumpAndSettle();
      expect(api.citySaved, ['Екатеринбург']);
      expect(find.textContaining('Город сохранён'), findsOneWidget);
    });

    testWidgets('an empty city is not sent', (t) async {
      await open(t);
      await reveal(t, find.text('Город для погоды'));
      await t.tap(find.text('Сохранить'));
      await t.pumpAndSettle();
      expect(api.citySaved, isEmpty);
      expect(find.text('Введи название города.'), findsOneWidget);
    });

    testWidgets('names to the model: a switch, saved at once', (t) async {
      await open(t);
      await reveal(t, find.textContaining('Названия приложений'));
      await t.tap(find.textContaining('Названия приложений'));
      await t.pumpAndSettle();
      expect(api.categorizeLlmSaved, [false]);
    });

    testWidgets('a load error is readable', (t) async {
      api = FakeApi()..sourcesError = const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: SourcesScreen(api: api, storage: MemoryStorage(), usage: FakeUsage(), scheduler: FakeScheduler()),
      ));
      await t.pumpAndSettle();
      expect(find.text('Нет связи с сервером. Проверь интернет.'), findsOneWidget);
    });
  });
}
