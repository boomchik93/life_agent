import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'models.dart';

enum ApiProblem { network, unauthorized, notConfigured, server }

/// An API failure with a message that can be shown to the user as is.
class ApiException implements Exception {
  const ApiException(this.problem, this.message);

  final ApiProblem problem;
  final String message;

  @override
  String toString() => message;
}

abstract class LifeApi {
  Future<WeekData> week({Wall? start});

  /// Saves today's check-in. [sleepHours] is null when not entered.
  Future<void> checkin({required int energy, required int mood, double? sleepHours});

  /// Cheap authenticated call used by the settings screen to test the connection.
  Future<void> ping();
}

class HttpLifeApi implements LifeApi {
  HttpLifeApi({required this.baseUrl, required this.token, http.Client? client})
      : _client = client ?? http.Client();

  final String baseUrl;
  final String token;
  final http.Client _client;

  static const _timeout = Duration(seconds: 30);

  Uri _uri(String path, [Map<String, String>? query]) {
    final base = Uri.parse(baseUrl);
    return base.replace(
      path: '${base.path.replaceAll(RegExp(r'/+$'), '')}$path',
      queryParameters: query,
    );
  }

  Future<Map<String, dynamic>> _send(String method, Uri uri, {Object? body}) async {
    final req = http.Request(method, uri)
      ..headers['Authorization'] = 'Bearer $token'
      ..headers['Accept'] = 'application/json';
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final http.Response res;
    try {
      res = await http.Response.fromStream(await _client.send(req).timeout(_timeout));
    } on TimeoutException {
      throw const ApiException(
          ApiProblem.network, 'Сервер не отвечает. Проверь интернет и попробуй ещё раз.');
    } on SocketException {
      throw const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    } on http.ClientException {
      throw const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    } on HandshakeException {
      throw const ApiException(
          ApiProblem.network, 'Не удалось установить защищённое соединение с сервером.');
    }

    switch (res.statusCode) {
      case 200:
        try {
          return jsonDecode(utf8.decode(res.bodyBytes)) as Map<String, dynamic>;
        } on FormatException {
          throw const ApiException(ApiProblem.server, 'Сервер вернул непонятный ответ.');
        }
      case 401:
        throw const ApiException(
            ApiProblem.unauthorized, 'Токен не подошёл. Проверь токен в настройках.');
      case 503:
        throw const ApiException(
            ApiProblem.notConfigured, 'На сервере не настроены календари.');
      case 400:
        throw ApiException(ApiProblem.server, _serverMessage(res) ?? 'Сервер отклонил запрос.');
      default:
        throw ApiException(ApiProblem.server,
            'Сервер недоступен или сломался (код ${res.statusCode}). Попробуй позже.');
    }
  }

  String? _serverMessage(http.Response res) {
    try {
      return (jsonDecode(utf8.decode(res.bodyBytes)) as Map)['error'] as String?;
    } catch (_) {
      return null;
    }
  }

  @override
  Future<WeekData> week({Wall? start}) async {
    final j = await _send('GET', _uri('/v1/week', start == null ? null : {'start': start.ymd}));
    try {
      return WeekData.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<void> checkin({required int energy, required int mood, double? sleepHours}) async {
    await _send('POST', _uri('/v1/checkin'), body: {
      'energy': energy,
      'mood': mood,
      'sleep_hours': ?sleepHours,
    });
  }

  @override
  Future<void> ping() async {
    await _send('GET', _uri('/v1/settings'));
  }
}
