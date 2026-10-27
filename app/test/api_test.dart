import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/llm_info.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/settings.dart';
import 'package:life_agent/slot.dart';

const _week = '''
{"from":"2026-10-26T00:00:00+05:00","to":"2026-11-02T00:00:00+05:00","events":[],"warnings":[],
 "risks":{"sleep":{"color":"gray","reasons":[],"metrics":{}},"overload":{"color":"green","reasons":[],"metrics":{}},
 "no_rest":{"color":"green","reasons":[],"metrics":{}},"state":{"color":"gray","reasons":[],"metrics":{}}}}''';

HttpLifeApi api(MockClient c, {String url = 'https://x.example/'}) =>
    HttpLifeApi(baseUrl: url, token: 'secret-token', client: c);

void main() {
  test('week sends the bearer token and the start date', () async {
    late http.Request seen;
    final a = api(MockClient((r) async {
      seen = r;
      return http.Response(_week, 200);
    }));
    await a.week(start: const Wall(2026, 11, 4));
    expect(seen.url.toString(), 'https://x.example/v1/week?start=2026-11-04');
    expect(seen.headers['Authorization'], 'Bearer secret-token');
  });

  test('base url with a path prefix and trailing slash', () async {
    late Uri seen;
    final a = api(MockClient((r) async {
      seen = r.url;
      return http.Response(_week, 200);
    }), url: 'https://x.example/life/');
    await a.week();
    expect(seen.toString(), 'https://x.example/life/v1/week');
  });

  test('checkin body omits sleep_hours when not entered', () async {
    late Map body;
    final a = api(MockClient((r) async {
      body = jsonDecode(r.body) as Map;
      return http.Response('{}', 200);
    }));
    await a.checkin(energy: 3, mood: 4);
    expect(body, {'energy': 3, 'mood': 4});
    await a.checkin(energy: 3, mood: 4, sleepHours: 7.5);
    expect(body['sleep_hours'], 7.5);
  });

  Future<ApiException> failure(MockClient c) async {
    try {
      await api(c).week();
    } on ApiException catch (e) {
      return e;
    }
    fail('expected ApiException');
  }

  test('maps failures to readable problems', () async {
    expect((await failure(MockClient((_) async => http.Response('{}', 401)))).problem, ApiProblem.unauthorized);
    expect((await failure(MockClient((_) async => http.Response('{}', 503)))).problem, ApiProblem.notConfigured);
    expect((await failure(MockClient((_) async => http.Response('oops', 502)))).problem, ApiProblem.server);
    expect((await failure(MockClient((_) async => http.Response('not json', 200)))).problem, ApiProblem.server);
    expect((await failure(MockClient((_) async => throw const SocketException('down')))).problem, ApiProblem.network);
    expect((await failure(MockClient((_) async => throw http.ClientException('down')))).problem, ApiProblem.network);
    expect((await failure(MockClient((_) async => http.Response('{"x":1}', 200)))).problem, ApiProblem.server);
  });

  test('400 passes the server message through', () async {
    final e = await failure(MockClient((_) async => http.Response('{"error":"energy must be 1..5"}', 400)));
    expect(e.message, 'energy must be 1..5');
  });

  test('errors never contain the token', () async {
    final e = await failure(MockClient((_) async => http.Response('{}', 401)));
    expect(e.message, isNot(contains('secret-token')));
  });

  group('validateBaseUrl', () {
    test('accepts https and loopback http only', () {
      expect(validateBaseUrl('https://life.144-31-165-229.sslip.io'), isNull);
      expect(validateBaseUrl('  https://x.example/life  '), isNull);
      expect(validateBaseUrl('http://127.0.0.1:18010'), isNull);
      expect(validateBaseUrl('http://localhost:18010'), isNull);
      expect(validateBaseUrl('http://life.example.com'), isNotNull);
      expect(validateBaseUrl('http://192.168.1.5:18010'), isNotNull);
      expect(validateBaseUrl(''), isNotNull);
      expect(validateBaseUrl('life.example.com'), isNotNull);
      expect(validateBaseUrl('ftp://x.example'), isNotNull);
    });
  });

  group('parse and llm', () {
    const card = '{"kind":"event","title":"КВН","category_guess":"hobby","date":"2026-10-30",'
        '"time_from":"18:00","time_to":"20:00","start":"2026-10-30T18:00:00+05:00",'
        '"end":"2026-10-30T20:00:00+05:00","duration_min":null,"deadline":null,"recurrence":null,'
        '"missing":[],"problems":[],"model":"v/m:free","calls":1}';

    test('parse posts the text and reads the card', () async {
      late http.Request seen;
      final a = api(MockClient((r) async {
        seen = r;
        return http.Response.bytes(utf8.encode(card), 200);
      }));
      final c = await a.parse('КВН в пятницу');
      expect(seen.method, 'POST');
      expect(seen.url.path, '/v1/parse');
      expect(jsonDecode(seen.body), {'text': 'КВН в пятницу'});
      expect(c.title, 'КВН');
      expect(c.timeFrom, '18:00');
    });

    test('daily limit, models down and not configured give their own messages', () async {
      Future<ApiException> fail(int status, String body) async {
        final a = api(MockClient((r) async => http.Response.bytes(utf8.encode(body), status)));
        try {
          await a.parse('x');
        } on ApiException catch (e) {
          return e;
        }
        throw StateError('no error');
      }

      final limit = await fail(429, '{"code":"daily_limit","error":"Сброс в 05:00.","reset_at":"x"}');
      expect(limit.problem, ApiProblem.dailyLimit);
      expect(limit.message, 'Сброс в 05:00.');

      final down = await fail(503, '{"code":"models_unavailable","error":"Модели не ответили."}');
      expect(down.problem, ApiProblem.llm);
      expect(down.message, 'Модели не ответили.');

      final calendars = await fail(503, '{"error":"calendars are not configured"}');
      expect(calendars.problem, ApiProblem.notConfigured);

      final rate = await fail(429, '{"error":"too many requests"}');
      expect(rate.problem, ApiProblem.server);
      expect(rate.message, contains('Подожди'));
    });

    test('saveLlmTasks puts model, effort and fallbacks under llm_tasks', () async {
      late http.Request seen;
      final a = api(MockClient((r) async {
        seen = r;
        return http.Response('{}', 200);
      }));
      await a.saveLlmTasks({
        'parse': const TaskModels(model: 'a/b:free', effort: 'low', fallbacks: ['c/d:free'], reasoning: true),
      });
      expect(seen.method, 'PUT');
      expect(seen.url.path, '/v1/settings');
      expect(jsonDecode(seen.body), {
        'llm_tasks': {
          'parse': {'model': 'a/b:free', 'effort': 'low', 'fallbacks': ['c/d:free']},
        },
      });
    });

    test('llmInfo reads tasks, models, quota and calls', () async {
      final a = api(MockClient((r) async => http.Response.bytes(
          utf8.encode('{"tasks":{"parse":{"model":"a/b","effort":"low","fallbacks":[],"reasoning":true}},'
              '"models":[{"id":"a/b","name":"B","free":true,"reasoning":true,"structured":true,"json_mode":true,"context_length":1}],'
              '"quota":{"used":2,"limit":50,"remaining":48,"reset_at":"2026-10-29T05:00:00+05:00"},'
              '"max_fallback":3,"recent_calls":[{"time":"2026-10-28T07:00:00Z","task":"parse","provider":"openrouter",'
              '"model":"a/b","latency_ms":900,"prompt_tokens":null,"completion_tokens":null,"result":"ok","attempt":1,"chain_pos":0,"source":"user"}],'
              '"warnings":[]}'),
          200)));
      final i = await a.llmInfo();
      expect(i.tasks['parse']!.reasoning, isTrue);
      expect(i.model('a/b')!.name, 'B');
      expect(i.quota!.remaining, 48);
      expect(i.quota!.resetAt.hm, '05:00');
      expect(i.recentCalls.single.result, 'ok');
    });
  });

  group('check and write', () {
    const slot = SlotRequest(
        title: 'КВН', start: Wall(2026, 10, 30, 18), end: Wall(2026, 10, 30, 20), category: 'hobby');
    const risks = '{"sleep":{"color":"gray","reasons":[],"metrics":{}},"overload":{"color":"green","reasons":[],"metrics":{}},'
        '"no_rest":{"color":"green","reasons":[],"metrics":{}},"state":{"color":"gray","reasons":[],"metrics":{}}}';
    const check = '{"ok":false,"needs_confirm":true,"category":"hobby","category_source":"guess","ask_category":true,'
        '"overlaps":[{"title":"Пара","start":"2026-10-30T17:00:00+05:00","end":"2026-10-30T19:00:00+05:00"}],'
        '"reasons":["Пересекается с «Пара» (17:00–19:00)."],"worsened":[],"before":$risks,"after":$risks}';

    test('check posts the slot and reads the verdict', () async {
      late http.Request seen;
      final a = api(MockClient((r) async {
        seen = r;
        return http.Response.bytes(utf8.encode(check), 200);
      }));
      final c = await a.check(slot);
      expect(seen.url.path, '/v1/check');
      expect((jsonDecode(seen.body) as Map)['start'], '2026-10-30T18:00');
      expect(c.needsConfirm, isTrue);
      expect(c.askCategory, isTrue);
      expect(c.overlaps.single.title, 'Пара');
      expect(c.overlaps.single.start.hm, '17:00');
      expect(c.after.overload.color, RiskColor.green);
    });

    test('writeEvent sends the Idempotency-Key and reads the item', () async {
      late http.Request seen;
      final a = api(MockClient((r) async {
        seen = r;
        return http.Response('{"item":{"id":7},"duplicate":true}', 200);
      }));
      final w = await a.writeEvent(slot, key: 'k-1234567890');
      expect(seen.url.path, '/v1/events');
      expect(seen.headers['Idempotency-Key'], 'k-1234567890');
      expect(w.id, 7);
      expect(w.duplicate, isTrue);
    });

    test('saveTask posts to items with the key', () async {
      late http.Request seen;
      final a = api(MockClient((r) async {
        seen = r;
        return http.Response('{"item":{"id":3},"duplicate":false}', 200);
      }));
      await a.saveTask(
          const TaskRequest(title: 'Курсовая', category: 'study', durationMin: 360, deadline: Wall(2026, 10, 30)),
          key: 'k-abcdefghij');
      expect(seen.url.path, '/v1/items');
      expect(seen.headers['Idempotency-Key'], 'k-abcdefghij');
      expect((jsonDecode(seen.body) as Map)['deadline'], '2026-10-30');
    });

    test('409 needs_confirm becomes NeedsConfirm with the check', () async {
      final a = api(MockClient((r) async => http.Response.bytes(
          utf8.encode('{"code":"needs_confirm","error":"x","check":$check}'), 409)));
      await expectLater(
        a.writeEvent(slot, key: 'k-1234567890'),
        throwsA(isA<NeedsConfirm>().having((e) => e.check.overlaps.length, 'overlaps', 1)),
      );
    });

    test('write problems keep the server words', () async {
      Future<ApiException> fail(int status, String body) async {
        final a = api(MockClient((r) async => http.Response.bytes(utf8.encode(body), status)));
        try {
          await a.writeEvent(slot, key: 'k-1234567890');
        } on ApiException catch (e) {
          return e;
        }
        throw StateError('no error');
      }

      final noAccess = await fail(502, '{"code":"no_write_access","error":"Нет права записи."}');
      expect(noAccess.message, 'Нет права записи.');
      expect(noAccess.problem, ApiProblem.server);
      final category = await fail(409, '{"code":"category_unconfirmed","error":"Сначала подтверди категорию события."}');
      expect(category.message, 'Сначала подтверди категорию события.');
    });
  });
}
