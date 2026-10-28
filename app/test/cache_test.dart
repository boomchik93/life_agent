import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/response_cache.dart';
import 'package:life_agent/theme.dart';
import 'package:life_agent/widgets/loadable.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _week = '''
{"from":"2026-10-26T00:00:00+05:00","to":"2026-11-02T00:00:00+05:00","events":[],"warnings":[],
 "risks":{"sleep":{"color":"gray","reasons":[],"metrics":{}},"overload":{"color":"green","reasons":[],"metrics":{}},
 "no_rest":{"color":"green","reasons":[],"metrics":{}},"state":{"color":"gray","reasons":[],"metrics":{}}}}''';

void main() {
  test('the answers live on after a restart, for the same server only', () async {
    SharedPreferences.setMockInitialValues({});
    final first = await ResponseCache.open('https://a.example');
    first.write('k', {'x': 1}, DateTime(2026, 10, 28, 9));
    await Future<void>.delayed(Duration.zero);

    final again = await ResponseCache.open('https://a.example');
    expect(again.read('k')?.value, {'x': 1});
    expect(again.read('k')?.at, DateTime(2026, 10, 28, 9));
    final other = await ResponseCache.open('https://b.example');
    expect(other.read('k'), isNull);
  });

  test('the oldest answers go when there are too many', () {
    final c = ResponseCache('s');
    for (var i = 0; i <= ResponseCache.maxEntries; i++) {
      c.write('k$i', {'i': i}, DateTime(2026));
    }
    expect(c.read('k0'), isNull);
    expect(c.read('k${ResponseCache.maxEntries}'), isNotNull);
  });

  test('the client keeps the week and gives it back', () async {
    final cache = ResponseCache('s');
    var now = DateTime(2026, 10, 28, 12);
    final api = HttpLifeApi(
      baseUrl: 'https://x.example/',
      token: 't',
      client: MockClient((_) async => http.Response(_week, 200)),
      cache: cache,
      now: () => now,
    );
    expect(api.keptWeek(), isNull);
    await api.week();
    expect(api.keptWeek()?.value.from.day, 26);
    expect(api.keptWeek()?.at, now);
    // On the next Monday the kept week is no longer "this week".
    now = DateTime(2026, 11, 2, 8);
    expect(api.keptWeek(), isNull);
  });

  test('a failed renewal keeps what is shown', () async {
    final l = Loadable<int>();
    await l.load(Future.value(1));
    final next = l.load(Future.error(const ApiException(ApiProblem.network, 'нет сети')));
    expect(l.data, 1);
    expect(l.loading, isTrue);
    await next;
    expect(l.data, 1);
    expect(l.error, isA<ApiException>());
    await l.load(Future.value(2), clear: true);
    expect(l.data, 2);
    expect(l.error, isNull);
  });

  test('a kept answer fills an empty screen until the new one comes', () async {
    final l = Loadable<int>();
    final pending = l.load(Future.delayed(const Duration(milliseconds: 5), () => 2), kept: Cached(1, DateTime(2026)));
    expect(l.data, 1);
    expect(l.keptAt, DateTime(2026));
    await pending;
    expect(l.data, 2);
    expect(l.keptAt, isNull);
  });

  test('an answer that was overtaken is dropped', () async {
    final l = Loadable<int>();
    final slow = l.load(Future.delayed(const Duration(milliseconds: 10), () => 1));
    await l.load(Future.value(2));
    await slow;
    expect(l.data, 2);
  });

  testWidgets('the data stays on the screen with a note when renewing failed', (t) async {
    final l = Loadable<String>();
    await t.pumpWidget(MaterialApp(
      theme: buildTheme(Palette.light),
      home: Scaffold(body: LoadableView<String>(source: l, onRetry: () {}, builder: (_, s) => Text(s))),
    ));
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await t.runAsync(() => l.load(Future.value('неделя')));
    await t.pump();
    expect(find.text('неделя'), findsOneWidget);

    await t.runAsync(() => l.load(Future.error(const ApiException(ApiProblem.network, 'нет сети'))));
    await t.pump();
    expect(find.text('неделя'), findsOneWidget);
    expect(find.text('Нет связи с сервером'), findsOneWidget);
    expect(find.text('Повторить'), findsOneWidget);
  });

  test('the time of a kept answer reads short', () {
    final now = DateTime(2026, 10, 28, 12);
    expect(keptLabel(DateTime(2026, 10, 28, 9, 5), now), '09:05');
    expect(keptLabel(DateTime(2026, 10, 27, 21, 0), now), 'вчера 21:00');
    expect(keptLabel(DateTime(2026, 10, 20, 8, 30), now), '20.10 08:30');
  });
}
