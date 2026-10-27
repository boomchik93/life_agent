import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/background.dart';
import 'package:life_agent/health.dart';
import 'package:life_agent/screens/health_screen.dart';

import 'fakes.dart';

class FakeSource implements HealthSource {
  HealthStatus state = HealthStatus.ready;
  bool bg = true;
  bool grants = true;
  bool installed = false;
  List<Sample> samples = [];
  int? stepCount;
  int hrv = 0;
  final stepRequests = <(DateTime, DateTime)>[];
  final reads = <(DateTime, DateTime)>[];

  @override
  Future<HealthStatus> status() async => state;
  @override
  Future<bool> requestAccess() async {
    if (grants) state = HealthStatus.ready;
    return grants;
  }

  @override
  Future<bool> backgroundAllowed() async => bg;
  @override
  Future<void> install() async => installed = true;
  @override
  Future<int?> steps(DateTime from, DateTime to) async {
    stepRequests.add((from, to));
    return stepCount;
  }

  @override
  Future<int> hrvDays(DateTime now) async => hrv;

  @override
  Future<List<Sample>> read(DateTime from, DateTime to) async {
    reads.add((from, to));
    return samples;
  }
}

class FakeScheduler implements SyncScheduler {
  int starts = 0, stops = 0;
  @override
  Future<void> start() async => starts++;
  @override
  Future<void> stop() async => stops++;
}

Sample stage(DateTime from, DateTime to) => Sample(SampleKind.sleepStage, from, to);
Sample session(DateTime from, DateTime to) => Sample(SampleKind.sleepSession, from, to);
Sample hr(DateTime at, double bpm) => Sample(SampleKind.heartRate, at, at, bpm);

final day = DateTime(2026, 10, 28);

void main() {
  group('summarizeDay', () {
    test('sleep is the union of the stages of the night that ended on the day', () {
      final d = summarizeDay(day, [
        stage(DateTime(2026, 10, 27, 23), DateTime(2026, 10, 28, 2)),
        stage(DateTime(2026, 10, 28, 2), DateTime(2026, 10, 28, 6, 30)),
        stage(DateTime(2026, 10, 28, 5), DateTime(2026, 10, 28, 6)), // overlap counts once
      ]);
      expect(d.date, '2026-10-28');
      expect(d.sleepHours, 7.5);
    });

    test('a session is used only when there are no stages', () {
      final s = session(DateTime(2026, 10, 27, 23), DateTime(2026, 10, 28, 6, 30));
      expect(summarizeDay(day, [s]).sleepHours, 7.5);
      final both = summarizeDay(day, [s, stage(DateTime(2026, 10, 28, 0), DateTime(2026, 10, 28, 5))]);
      expect(both.sleepHours, 5, reason: 'stages win over the session that also covers awake time');
    });

    test('another night does not count, a nap does', () {
      final d = summarizeDay(day, [
        stage(DateTime(2026, 10, 26, 23), DateTime(2026, 10, 27, 7)), // the night before: its own day
        stage(DateTime(2026, 10, 28, 14), DateTime(2026, 10, 28, 15)), // a nap ends on the day
      ]);
      expect(d.sleepHours, 1, reason: 'the nap is counted, the other night is not');
    });

    test('a night that began before 18:00 the previous day is ignored', () {
      expect(summarizeDay(day, [stage(DateTime(2026, 10, 27, 12), DateTime(2026, 10, 28, 1))]).sleepHours, isNull);
    });

    test('heart rate is the mean of the day, outliers are dropped', () {
      final d = summarizeDay(day, [
        hr(DateTime(2026, 10, 28, 9), 60),
        hr(DateTime(2026, 10, 28, 12), 80),
        hr(DateTime(2026, 10, 28, 13), 400), // sensor glitch
        hr(DateTime(2026, 10, 27, 12), 120), // another day
      ]);
      expect(d.avgHr, 70);
      expect(d.sleepHours, isNull);
    });

    test('resting heart rate, workouts and steps join the summary', () {
      final d = summarizeDay(
        day,
        [
          Sample(SampleKind.restingHeartRate, DateTime(2026, 10, 28, 6), DateTime(2026, 10, 28, 6), 56),
          Sample(SampleKind.restingHeartRate, DateTime(2026, 10, 28, 7), DateTime(2026, 10, 28, 7), 400), // glitch
          Sample(SampleKind.workout, DateTime(2026, 10, 28, 18), DateTime(2026, 10, 28, 18, 40)),
          Sample(SampleKind.workout, DateTime(2026, 10, 28, 18, 10), DateTime(2026, 10, 28, 18, 50)), // the watch repeats it
          Sample(SampleKind.workout, DateTime(2026, 10, 27, 18), DateTime(2026, 10, 27, 19)), // yesterday
        ],
        steps: 8421,
      );
      expect(d.restingHr, 56);
      expect(d.workoutMin, 50, reason: 'overlapping records are one workout');
      expect(d.steps, 8421);
      expect(d.toJson(), {'date': '2026-10-28', 'resting_hr': 56, 'steps': 8421, 'workout_min': 50});
    });

    test('zero steps and no workouts are unknown, not zero', () {
      final d = summarizeDay(day, const [], steps: 0);
      expect(d.isEmpty, isTrue);
      expect(d.toJson(), {'date': '2026-10-28'});
    });

    test('nothing in, nothing out; the body leaves out unknown fields', () {
      final d = summarizeDay(day, const []);
      expect(d.isEmpty, isTrue);
      expect(const HealthDay(date: '2026-10-28', sleepHours: 7).toJson(), {'date': '2026-10-28', 'sleep_hours': 7});
    });
  });

  group('HealthSync', () {
    final now = DateTime(2026, 10, 28, 13);

    test('sends yesterday and today, only what has data', () async {
      final api = FakeApi();
      final src = FakeSource()
        ..samples = [
          stage(DateTime(2026, 10, 27, 23), DateTime(2026, 10, 28, 6)),
          hr(DateTime(2026, 10, 28, 10), 66),
        ];
      final r = await HealthSync(api: api, source: src, now: () => now).run();
      expect(r.sent, 1);
      expect(api.healthSent.single.toJson(), {'date': '2026-10-28', 'sleep_hours': 7, 'avg_hr': 66});
      expect(src.reads.single.$1, DateTime(2026, 10, 26, 18), reason: 'covers the night before yesterday');
    });

    test('without access or Health Connect nothing is read or sent', () async {
      for (final st in [HealthStatus.unsupported, HealthStatus.notInstalled, HealthStatus.needsAccess]) {
        final api = FakeApi();
        final src = FakeSource()..state = st;
        final r = await HealthSync(api: api, source: src, now: () => now).run();
        expect(r.sent, 0);
        expect(r.skipped, isNotNull);
        expect(src.reads, isEmpty);
        expect(api.healthSent, isEmpty);
      }
    });

    test('the steps of each day are asked for the whole day and sent', () async {
      final api = FakeApi();
      final src = FakeSource()
        ..stepCount = 7000
        ..samples = [Sample(SampleKind.workout, DateTime(2026, 10, 28, 9), DateTime(2026, 10, 28, 9, 30))];
      final r = await HealthSync(api: api, source: src, now: () => now).run();
      expect(r.sent, 2);
      expect(api.healthSent.map((d) => d.steps), [7000, 7000]);
      expect(api.healthSent.last.workoutMin, 30);
      expect(src.stepRequests.first.$1, DateTime(2026, 10, 27));
      expect(src.stepRequests.first.$2, DateTime(2026, 10, 28), reason: 'a finished day ends at midnight');
      expect(src.stepRequests.last.$2, now, reason: 'today ends now');
    });

    test('empty Health Connect is reported, not sent as zeros', () async {
      final api = FakeApi();
      final r = await HealthSync(api: api, source: FakeSource(), now: () => now).run();
      expect(r.sent, 0);
      expect(r.skipped, contains('нет сна и пульса'));
      expect(api.healthSent, isEmpty);
    });

    test('a server error reaches the caller', () async {
      final api = FakeApi()..healthError = const ApiException(ApiProblem.network, 'Нет связи');
      final src = FakeSource()..samples = [hr(DateTime(2026, 10, 28, 10), 66)];
      expect(HealthSync(api: api, source: src, now: () => now).run(), throwsA(isA<ApiException>()));
    });
  });

  group('HealthScreen', () {
    Future<(FakeApi, FakeSource, FakeScheduler, MemoryStorage)> open(WidgetTester t,
        {HealthStatus state = HealthStatus.ready, bool bg = true}) async {
      final api = FakeApi();
      final src = FakeSource()
        ..state = state
        ..bg = bg
        ..samples = [stage(DateTime(2026, 10, 27, 23), DateTime(2026, 10, 28, 6))];
      final sched = FakeScheduler();
      final storage = MemoryStorage();
      await t.pumpWidget(MaterialApp(
        locale: const Locale('ru'),
        supportedLocales: const [Locale('ru')],
        localizationsDelegates: GlobalMaterialLocalizations.delegates,
        home: HealthScreen(
            api: api, storage: storage, source: src, scheduler: sched, now: () => DateTime(2026, 10, 28, 13)),
      ));
      await t.pumpAndSettle();
      return (api, src, sched, storage);
    }

    testWidgets('no access: ask for it, then the controls appear', (t) async {
      final (_, src, _, _) = await open(t, state: HealthStatus.needsAccess);
      expect(find.text('Нужен доступ к сну и пульсу.'), findsOneWidget);
      expect(find.text('Отправить сейчас'), findsNothing);
      await t.tap(find.text('Дать доступ'));
      await t.pumpAndSettle();
      expect(src.state, HealthStatus.ready);
      expect(find.text('Отправить сейчас'), findsOneWidget);
    });

    testWidgets('refused access is explained', (t) async {
      final (_, src, _, _) = await open(t, state: HealthStatus.needsAccess);
      src.grants = false;
      await t.tap(find.text('Дать доступ'));
      await t.pumpAndSettle();
      expect(find.textContaining('Доступ не выдан'), findsOneWidget);
    });

    testWidgets('Health Connect missing: offer the install', (t) async {
      final (_, src, _, _) = await open(t, state: HealthStatus.notInstalled);
      await t.tap(find.text('Установить'));
      await t.pumpAndSettle();
      expect(src.installed, isTrue);
    });

    testWidgets('send now sends the summary and remembers the time', (t) async {
      final (api, _, _, storage) = await open(t);
      await t.tap(find.text('Отправить сейчас'));
      await t.pumpAndSettle();
      expect(api.healthSent.single.sleepHours, 7);
      expect(find.text('Отправлено дней: 1.'), findsOneWidget);
      expect(storage.healthAt, startsWith('2026-10-28T13:00'));
    });

    testWidgets('the switch starts and stops the background task and is remembered', (t) async {
      final (_, _, sched, storage) = await open(t);
      await t.tap(find.byType(Switch));
      await t.pumpAndSettle();
      expect(sched.starts, 1);
      expect(storage.healthOn, isTrue);
      await t.tap(find.byType(Switch));
      await t.pumpAndSettle();
      expect(sched.stops, 1);
      expect(storage.healthOn, isFalse);
    });

    testWidgets('without the background permission the screen says so', (t) async {
      await open(t, bg: false);
      expect(find.textContaining('Фоновое чтение не разрешено'), findsOneWidget);
    });

    testWidgets('the HRV check says what the watch gives', (t) async {
      final (_, src, _, _) = await open(t);
      await t.scrollUntilVisible(find.text('Проверить HRV и стресс'), 200);
      src.hrv = 0;
      await t.tap(find.text('Проверить HRV и стресс'));
      await t.pumpAndSettle();
      expect(find.textContaining('HRV и стресс не используем'), findsOneWidget);
      src.hrv = 5;
      await t.tap(find.text('Проверить HRV и стресс'));
      await t.pumpAndSettle();
      expect(find.textContaining('HRV есть за 5 из 7 дней'), findsOneWidget);
    });

    testWidgets('deleting asks first, then clears the server', (t) async {
      final (api, _, _, _) = await open(t);
      await t.scrollUntilVisible(find.text('Удалить данные здоровья'), 200);
      await t.tap(find.text('Удалить данные здоровья'));
      await t.pumpAndSettle();
      expect(find.text('Удалить данные здоровья?'), findsOneWidget);
      await t.tap(find.text('Отмена'));
      await t.pumpAndSettle();
      expect(api.healthDeleteCalled, isFalse);

      await t.tap(find.text('Удалить данные здоровья'));
      await t.pumpAndSettle();
      await t.tap(find.text('Удалить'));
      await t.pumpAndSettle();
      expect(api.healthDeleteCalled, isTrue);
      expect(find.text('Удалено сводок: 3.'), findsOneWidget);
    });

    testWidgets('the battery hint is on the screen', (t) async {
      await open(t);
      expect(find.text('Чтобы фон не убивался'), findsOneWidget);
    });
  });
}
