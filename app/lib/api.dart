import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'advice.dart';
import 'card.dart';
import 'format.dart';
import 'health.dart';
import 'llm_info.dart';
import 'models.dart';
import 'sources.dart';
import 'timetrack.dart';
import 'update.dart';
import 'usage.dart';
import 'plan.dart';
import 'response_cache.dart';
import 'review.dart';
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

  /// Asks for an advice now. Takes up to a minute and a half on slow models.
  Future<Advice> requestAdvice();

  /// The history of advice, newest first.
  Future<List<Advice>> adviceHistory();

  /// The advice the server prepared for a notification, if there is one. It
  /// is handed over once.
  Future<Advice?> pendingAdvice();

  Future<AdviceSettings> adviceSettings();
  Future<void> saveAdviceSettings(AdviceSettings s);

  /// Categories of app names the phone could not place itself. [llm] false
  /// keeps the names away from the model for this call.
  Future<Map<String, NameCategory>> categorize(List<String> names, {bool llm = true});

  /// Sends the minutes per category of one day from the phone or the Mac.
  Future<void> sendActivityDay(UsageDay day, {required String source});

  Future<List<SourceInfo>> sources();
  Future<void> setSourceEnabled(String source, bool enabled);

  /// Deletes the data of one source on the server. Returns the rows removed.
  Future<int> deleteSourceData(String source);

  /// Everything the server stores about the user, as JSON text.
  Future<String> exportData();

  /// Deletes all data on the server (the settings stay).
  Future<void> deleteAllData();

  /// The city for the weather forecast; empty when none is set.
  Future<String> weatherCity();
  Future<void> saveWeatherCity(String city);

  /// Whether names of unknown apps may go to the language model.
  Future<bool> categorizeLlm();
  Future<void> saveCategorizeLlm(bool on);

  /// Sends the summary of one day from Health Connect.
  Future<void> sendHealthDay(HealthDay day);

  /// Deletes every health summary on the server. Returns how many there were.
  Future<int> deleteHealth();

  /// Builds the week plan from the open tasks, and with [goals] also from the
  /// minutes the goals are still missing. Nothing is written.
  Future<PlanData> planWeek({bool goals = false});

  /// The four goals with their norms and suggestions.
  Future<Goals> goals();

  /// Answers a suggestion ("confirm", "keep"), sets a norm ("set", with
  /// [normMin]) or removes it ("clear").
  Future<Goals> answerGoal(String goal, String action, {int? normMin});

  /// The weekly review; the current week unless [start] is given.
  Future<Review> reviewWeek({Wall? start});

  /// Writes the plan the user confirmed. The same [key] never writes twice.
  Future<PlanApplied> applyPlan(PlanData plan, {required String key});

  /// Cheap authenticated call used by the settings screen to test the connection.
  Future<void> ping();
}

/// Answers kept from an earlier time, for a screen to show at once while it
/// asks the server again. Null when there is none.
abstract interface class CachedReads {
  Cached<WeekData>? keptWeek({Wall? start});
  Cached<List<RiskDay>>? keptRiskHistory({int days = 7});
  Cached<TimeOverview>? keptTimeOverview(DateTime from, DateTime to, {bool compare = false});
  Cached<ActivitiesData>? keptActivities({String range = 'month'});
  Cached<Goals>? keptGoals();
  Cached<Review>? keptReview({Wall? start});
}

class HttpLifeApi implements LifeApi, TimeApi, CachedReads, UpdateApi {
  HttpLifeApi({required this.baseUrl, required this.token, http.Client? client, this.cache, DateTime Function()? now})
      : _client = client ?? http.Client(),
        _now = now ?? DateTime.now;

  final String baseUrl;
  final String token;
  final http.Client _client;

  /// Where the answers of the screens are kept; null: nowhere.
  final ResponseCache? cache;
  final DateTime Function() _now;

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
      case 404 when _serverField(res, 'code') == 'no_release':
        throw const _NoRelease();
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

  /// A GET whose answer is kept for the next start of the screen.
  Future<Map<String, dynamic>> _getKept(Uri uri) async {
    final j = await _send('GET', uri);
    cache?.write(uri.toString(), j, _now());
    return j;
  }

  /// The kept answer of [uri] read by [read]; null when there is none or it
  /// no longer reads.
  Cached<T>? _kept<T>(Uri uri, T Function(Map<String, dynamic>) read) {
    final c = cache?.read(uri.toString());
    if (c == null) return null;
    try {
      return Cached(read(c.value), c.at);
    } catch (_) {
      return null;
    }
  }

  Uri _weekUri(Wall? start) => _uri('/v1/week', start == null ? null : {'start': start.ymd});

  @override
  Cached<WeekData>? keptWeek({Wall? start}) {
    final kept = _kept(_weekUri(start), WeekData.fromJson);
    if (kept == null || start != null) return kept;
    // "This week" of an earlier week is not shown as this one.
    final today = wallNow(_now()).dayStart;
    return !today.isBefore(kept.value.from) && today.isBefore(kept.value.to) ? kept : null;
  }

  Uri _riskHistoryUri(int days) => _uri('/v1/risks/history', {'days': '$days'});

  @override
  Cached<List<RiskDay>>? keptRiskHistory({int days = 7}) => _kept(_riskHistoryUri(days), _riskDays);

  Uri _timeOverviewUri(DateTime from, DateTime to, bool compare) =>
      _uri('/v1/time/overview', {'from': _day(from), 'to': _day(to), if (compare) 'compare': '1'});

  @override
  Cached<TimeOverview>? keptTimeOverview(DateTime from, DateTime to, {bool compare = false}) =>
      _kept(_timeOverviewUri(from, to, compare), TimeOverview.fromJson);

  Uri _activitiesUri(String range) => _uri('/v1/activities', {'range': range});

  @override
  Cached<ActivitiesData>? keptActivities({String range = 'month'}) => _kept(_activitiesUri(range), ActivitiesData.fromJson);

  @override
  Cached<Goals>? keptGoals() => _kept(_uri('/v1/goals'), Goals.fromJson);

  Uri _reviewUri(Wall? start) => _uri('/v1/review/week', start == null ? null : {'start': start.ymd});

  @override
  Cached<Review>? keptReview({Wall? start}) => _kept(_reviewUri(start), Review.fromJson);

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
    final j = await _getKept(_weekUri(start));
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

  @override
  Future<Advice> requestAdvice() async {
    final j = await _send('POST', _uri('/v1/advice'), body: <String, dynamic>{}, timeout: _parseTimeout);
    try {
      return Advice.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<List<Advice>> adviceHistory() async {
    final j = await _send('GET', _uri('/v1/advice'));
    try {
      return [for (final a in (j['advice'] as List)) Advice.fromJson(a as Map<String, dynamic>)];
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<Advice?> pendingAdvice() async {
    final j = await _send('GET', _uri('/v1/advice/pending'));
    final a = j['advice'];
    try {
      return a == null ? null : Advice.fromJson(a as Map<String, dynamic>);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<AdviceSettings> adviceSettings() async {
    final j = await _send('GET', _uri('/v1/settings'));
    final a = j['advice'];
    return AdviceSettings(includePrivate: a is Map && a['include_private'] == true);
  }

  @override
  Future<void> saveAdviceSettings(AdviceSettings s) async {
    await _send('PUT', _uri('/v1/settings'), body: {
      'advice': {'include_private': s.includePrivate},
    });
  }

  @override
  Future<Map<String, NameCategory>> categorize(List<String> names, {bool llm = true}) async {
    final out = <String, NameCategory>{};
    // The server takes up to 100 names in one request.
    for (var i = 0; i < names.length; i += 100) {
      final chunk = names.sublist(i, i + 100 > names.length ? names.length : i + 100);
      final j = await _send('POST', _uri('/v1/categorize'), body: {'names': chunk, 'llm': llm}, timeout: _parseTimeout);
      try {
        for (final e in (j['categories'] as Map<String, dynamic>).entries) {
          final v = e.value as Map<String, dynamic>;
          out[e.key] = NameCategory(
            category: v['category'] as String? ?? 'unknown',
            confidence: (v['confidence'] as num?)?.toDouble() ?? 0,
            resolved: v['resolved'] as bool? ?? false,
          );
        }
      } catch (_) {
        throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
      }
    }
    return out;
  }

  @override
  Future<void> sendActivityDay(UsageDay day, {required String source}) async {
    await _send('POST', _uri('/v1/activity/daily'), body: day.toJson(source));
  }

  @override
  Future<List<SourceInfo>> sources() async {
    final j = await _send('GET', _uri('/v1/sources'));
    try {
      return [for (final s in (j['sources'] as List)) SourceInfo.fromJson(s as Map<String, dynamic>)];
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<void> setSourceEnabled(String source, bool enabled) async {
    await _send('PUT', _uri('/v1/sources/$source'), body: {'enabled': enabled});
  }

  @override
  Future<int> deleteSourceData(String source) async {
    final j = await _send('DELETE', _uri('/v1/activity', {'source': source}));
    return (j['deleted'] as num?)?.toInt() ?? 0;
  }

  @override
  Future<String> exportData() async => jsonEncode(await _send('GET', _uri('/v1/export'), timeout: _parseTimeout));

  @override
  Future<void> deleteAllData() async {
    await _send('DELETE', _uri('/v1/data'), body: {'confirm': 'delete-all-data'});
    cache?.clear();
  }

  @override
  Future<String> weatherCity() async {
    final j = await _send('GET', _uri('/v1/settings'));
    final w = j['weather'];
    return w is Map ? (w['city'] as String? ?? '') : '';
  }

  @override
  Future<void> saveWeatherCity(String city) async {
    await _send('PUT', _uri('/v1/settings'), body: {
      'weather': {'city': city.trim()},
    });
  }

  @override
  Future<bool> categorizeLlm() async {
    final j = await _send('GET', _uri('/v1/settings'));
    final c = j['categorize'];
    return !(c is Map && c['llm'] == false);
  }

  @override
  Future<void> saveCategorizeLlm(bool on) async {
    await _send('PUT', _uri('/v1/settings'), body: {
      'categorize': {'llm': on},
    });
  }

  @override
  Future<void> sendHealthDay(HealthDay day) async {
    await _send('POST', _uri('/v1/health/daily'), body: day.toJson());
  }

  @override
  Future<int> deleteHealth() async {
    final j = await _send('DELETE', _uri('/v1/health'));
    return (j['deleted'] as num?)?.toInt() ?? 0;
  }

  @override
  Future<Goals> goals() async {
    final j = await _getKept(_uri('/v1/goals'));
    try {
      return Goals.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<Goals> answerGoal(String goal, String action, {int? normMin}) async {
    final j = await _send('PUT', _uri('/v1/goals'), body: {'goal': goal, 'action': action, 'norm_min': ?normMin});
    try {
      return Goals.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<Review> reviewWeek({Wall? start}) async {
    final j = await _getKept(_reviewUri(start));
    try {
      return Review.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<PlanData> planWeek({bool goals = false}) async {
    final j = await _send('POST', _uri('/v1/plan/week'), body: <String, dynamic>{if (goals) 'goals': true});
    try {
      return PlanData.fromJson(j).withGoals(goals);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<PlanApplied> applyPlan(PlanData plan, {required String key}) async {
    final j = await _send('POST', _uri('/v1/plan/week/apply'), body: plan.applyJson(), idempotencyKey: key);
    try {
      return PlanApplied.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  Written _written(Map<String, dynamic> j) {
    try {
      return Written.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  // ---- the "Время" tab ----

  String _day(DateTime d) =>
      '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  T _unexpected<T>(T Function() read) {
    try {
      return read();
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  @override
  Future<TimeOverview> timeOverview(DateTime from, DateTime to, {bool compare = false}) async {
    final j = await _getKept(_timeOverviewUri(from, to, compare));
    return _unexpected(() => TimeOverview.fromJson(j));
  }

  @override
  Future<CategoryDetail> timeCategory(String category, DateTime from, DateTime to) async {
    final j = await _send('GET', _uri('/v1/time/category', {'category': category, 'from': _day(from), 'to': _day(to)}));
    return _unexpected(() => CategoryDetail.fromJson(j));
  }

  @override
  Future<ActivitiesData> activities({String range = 'month'}) async {
    final j = await _getKept(_activitiesUri(range));
    return _unexpected(() => ActivitiesData.fromJson(j));
  }

  @override
  Future<void> setAppCategory(String name, String category) async {
    await _send('PUT', _uri('/v1/apps/category'), body: {'name': name, 'category': category});
  }

  @override
  Future<List<RiskDay>> riskHistory({int days = 7}) async {
    final j = await _getKept(_riskHistoryUri(days));
    return _unexpected(() => _riskDays(j));
  }

  static List<RiskDay> _riskDays(Map<String, dynamic> j) =>
      [for (final d in (j['days'] as List)) RiskDay.fromJson(d as Map<String, dynamic>)];

  @override
  Future<void> ping() async {
    await _send('GET', _uri('/v1/settings'));
  }

  @override
  Future<AppRelease?> latestRelease() async {
    final Map<String, dynamic> j;
    try {
      j = await _send('GET', _uri('/v1/app/latest'));
    } on _NoRelease {
      return null;
    }
    try {
      return AppRelease.fromJson(j);
    } catch (_) {
      throw const ApiException(ApiProblem.server, 'Сервер вернул неожиданные данные. Обнови приложение.');
    }
  }

  // Between two pieces of the APK; a stalled download ends instead of hanging.
  static const _downloadIdle = Duration(seconds: 60);

  @override
  Future<void> downloadRelease(AppRelease release, String path,
      {void Function(int got, int total)? onProgress}) async {
    final req = http.Request('GET', _uri('/v1/app/apk'))..headers['Authorization'] = 'Bearer $token';
    final file = File(path);
    final hash = Sha256Sink();
    IOSink? out;
    var got = 0;
    try {
      final res = await _client.send(req).timeout(_timeout);
      if (res.statusCode == 401) {
        throw const ApiException(ApiProblem.unauthorized, 'Токен не подошёл. Проверь токен в настройках.');
      }
      if (res.statusCode != 200) {
        throw ApiException(ApiProblem.server, 'Сервер не отдал обновление (код ${res.statusCode}). Попробуй позже.');
      }
      out = file.openWrite();
      await for (final chunk in res.stream.timeout(_downloadIdle)) {
        out.add(chunk);
        hash.add(chunk);
        got += chunk.length;
        onProgress?.call(got, release.size);
      }
      await out.close();
      out = null;
      hash.close();
    } on TimeoutException {
      await _discard(out, file);
      throw const ApiException(ApiProblem.network, 'Скачивание оборвалось. Проверь интернет и попробуй ещё раз.');
    } on SocketException {
      await _discard(out, file);
      throw const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    } on http.ClientException {
      await _discard(out, file);
      throw const ApiException(ApiProblem.network, 'Нет связи с сервером. Проверь интернет.');
    } on HandshakeException {
      await _discard(out, file);
      throw const ApiException(ApiProblem.network, 'Не удалось установить защищённое соединение с сервером.');
    } catch (_) {
      await _discard(out, file);
      rethrow;
    }
    if (got != release.size || hash.hex != release.sha256) {
      await _discard(null, file);
      throw const ApiException(ApiProblem.server, 'Скачанный файл повреждён. Попробуй ещё раз.');
    }
  }

  Future<void> _discard(IOSink? out, File file) async {
    try {
      await out?.close();
    } catch (_) {}
    try {
      if (await file.exists()) await file.delete();
    } catch (_) {}
  }
}

/// `GET /v1/app/latest` when the server has no APK to hand out.
class _NoRelease implements Exception {
  const _NoRelease();
}
