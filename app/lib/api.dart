import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'card.dart';
import 'llm_info.dart';
import 'models.dart';
import 'slot.dart';

enum ApiProblem { network, unauthorized, notConfigured, server, dailyLimit, llm }

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

  /// Turns a phrase into a card. Takes up to a minute and a half on slow models.
  Future<ParsedCard> parse(String text);

  /// Models, chosen configuration and the free quota for the settings screen.
  Future<LlmInfo> llmInfo();

  /// Saves the model, effort and fallback chain per task (`parse`, `advice`).
  Future<void> saveLlmTasks(Map<String, TaskModels> tasks);

  /// Can the event be added: overlaps, risks before and after, category.
  Future<CheckResult> check(SlotRequest slot);

  /// Writes the event to the calendar. Throws [NeedsConfirm] when the server
  /// wants the user's OK first. The same [key] never writes twice.
  Future<Written> writeEvent(SlotRequest slot, {required String key});

  /// Saves a task with a deadline. The same [key] never saves twice.
  Future<Written> saveTask(TaskRequest task, {required String key});

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
  // The server waits for the models up to 85 s.
  static const _parseTimeout = Duration(seconds: 95);

  Uri _uri(String path, [Map<String, String>? query]) {
    final base = Uri.parse(baseUrl);
    return base.replace(
      path: '${base.path.replaceAll(RegExp(r'/+$'), '')}$path',
      queryParameters: query,
    );
  }

  Future<Map<String, dynamic>> _send(String method, Uri uri,
      {Object? body, String? idempotencyKey, Duration timeout = _timeout}) async {
    final req = http.Request(method, uri)
      ..headers['Authorization'] = 'Bearer $token'
      ..headers['Accept'] = 'application/json';
    if (idempotencyKey != null) req.headers['Idempotency-Key'] = idempotencyKey;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final http.Response res;
    try {
      res = await http.Response.fromStream(await _client.send(req).timeout(timeout));
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
      case 400:
        throw ApiException(ApiProblem.server, _serverMessage(res) ?? 'Сервер отклонил запрос.');
      case 409:
        final code = _serverField(res, 'code');
        if (code == 'needs_confirm') {
          try {
            final check = (jsonDecode(utf8.decode(res.bodyBytes)) as Map)['check'] as Map<String, dynamic>;
            throw NeedsConfirm(CheckResult.fromJson(check));
          } on NeedsConfirm {
            rethrow;
          } catch (_) {
            // fall through to the plain message
          }
        }
        throw ApiException(ApiProblem.server, _serverMessage(res) ?? 'Сервер отклонил запрос.');
      case 429:
        final code = _serverField(res, 'code');
        if (code == 'daily_limit') {
          throw ApiException(ApiProblem.dailyLimit,
              _serverMessage(res) ?? 'Дневной лимит бесплатных моделей исчерпан.');
        }
        throw const ApiException(ApiProblem.server, 'Слишком много запросов. Подожди минуту.');
      case 502:
      case 503:
      case 504:
        // The models failed or are not chosen: the server explains it in words.
        final code = _serverField(res, 'code');
        if (code != null) {
          final models = code.startsWith('llm') || code.startsWith('models') || code == 'timeout';
          throw ApiException(models ? ApiProblem.llm : ApiProblem.server,
              _serverMessage(res) ?? 'Сервер не смог выполнить запрос.');
        }
        if (res.statusCode == 503) {
          throw const ApiException(ApiProblem.notConfigured, 'На сервере не настроены календари.');
        }
        throw ApiException(ApiProblem.server,
            'Сервер недоступен или сломался (код ${res.statusCode}). Попробуй позже.');
      default:
        throw ApiException(ApiProblem.server,
            'Сервер недоступен или сломался (код ${res.statusCode}). Попробуй позже.');
    }
  }

  String? _serverField(http.Response res, String key) {
    try {
      return (jsonDecode(utf8.decode(res.bodyBytes)) as Map)[key] as String?;
    } catch (_) {
      return null;
    }
  }

  String? _serverMessage(http.Response res) => _serverField(res, 'error');

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
  Future<ParsedCard> parse(String text) async {
    final j = await _send('POST', _uri('/v1/parse'), body: {'text': text}, timeout: _parseTimeout);
    try {
      return ParsedCard.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<LlmInfo> llmInfo() async {
    final j = await _send('GET', _uri('/v1/llm'));
    try {
      return LlmInfo.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<void> saveLlmTasks(Map<String, TaskModels> tasks) async {
    await _send('PUT', _uri('/v1/settings'), body: {
      'llm_tasks': {for (final e in tasks.entries) e.key: e.value.toJson()},
    });
  }

  @override
  Future<CheckResult> check(SlotRequest slot) async {
    final j = await _send('POST', _uri('/v1/check'), body: slot.toJson());
    try {
      return CheckResult.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<Written> writeEvent(SlotRequest slot, {required String key}) async =>
      _written(await _send('POST', _uri('/v1/events'), body: slot.toJson(), idempotencyKey: key));

  @override
  Future<Written> saveTask(TaskRequest task, {required String key}) async =>
      _written(await _send('POST', _uri('/v1/items'), body: task.toJson(), idempotencyKey: key));

  Written _written(Map<String, dynamic> j) {
    try {
      return Written.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<void> ping() async {
    await _send('GET', _uri('/v1/settings'));
  }
}
