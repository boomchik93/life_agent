import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:life_agent/api.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/settings.dart';

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
}
