import 'models.dart';

const llmTaskNames = {'parse': 'Разбор фраз', 'advice': 'Совет'};

const efforts = ['low', 'medium', 'high'];
const effortNames = {'low': 'Низкий', 'medium': 'Средний', 'high': 'Высокий'};

class ModelInfo {
  const ModelInfo({
    required this.id,
    required this.name,
    required this.free,
    required this.reasoning,
    required this.structured,
  });

  final String id, name;
  final bool free, reasoning, structured;

  factory ModelInfo.fromJson(Map<String, dynamic> j) => ModelInfo(
        id: j['id'] as String,
        name: j['name'] as String? ?? j['id'] as String,
        free: j['free'] as bool? ?? false,
        reasoning: j['reasoning'] as bool? ?? false,
        structured: j['structured'] as bool? ?? false,
      );
}

/// The model, effort and fallback chain of one task.
class TaskModels {
  const TaskModels({
    required this.model,
    required this.effort,
    required this.fallbacks,
    required this.reasoning,
  });

  final String model, effort;
  final List<String> fallbacks;

  /// The chosen model takes an effort, so the control is shown.
  final bool reasoning;

  factory TaskModels.fromJson(Map<String, dynamic> j) => TaskModels(
        model: j['model'] as String? ?? '',
        effort: j['effort'] as String? ?? 'low',
        fallbacks: [for (final f in (j['fallbacks'] as List? ?? const [])) f as String],
        reasoning: j['reasoning'] as bool? ?? false,
      );

  TaskModels copyWith({String? model, String? effort, List<String>? fallbacks, bool? reasoning}) =>
      TaskModels(
        model: model ?? this.model,
        effort: effort ?? this.effort,
        fallbacks: fallbacks ?? this.fallbacks,
        reasoning: reasoning ?? this.reasoning,
      );

  @override
  bool operator ==(Object other) =>
      other is TaskModels &&
      other.model == model &&
      other.effort == effort &&
      other.reasoning == reasoning &&
      other.fallbacks.length == fallbacks.length &&
      [for (var i = 0; i < fallbacks.length; i++) fallbacks[i] == other.fallbacks[i]]
          .every((e) => e);

  @override
  int get hashCode => Object.hash(model, effort, reasoning, fallbacks.join(','));

  Map<String, dynamic> toJson() => {'model': model, 'effort': effort, 'fallbacks': fallbacks};
}

class Quota {
  const Quota({required this.used, required this.limit, required this.remaining, required this.resetAt});

  final int used, limit, remaining;
  final Wall resetAt;

  factory Quota.fromJson(Map<String, dynamic> j) => Quota(
        used: (j['used'] as num).toInt(),
        limit: (j['limit'] as num).toInt(),
        remaining: (j['remaining'] as num).toInt(),
        resetAt: Wall.parse(j['reset_at'] as String),
      );
}

class LlmCall {
  const LlmCall({
    required this.time,
    required this.task,
    required this.model,
    required this.result,
    required this.source,
    required this.attempt,
    required this.chainPos,
    required this.latencyMs,
  });

  final Wall time;
  final String task, model, result, source;
  final int attempt, chainPos, latencyMs;

  factory LlmCall.fromJson(Map<String, dynamic> j) => LlmCall(
        time: Wall.parse(j['time'] as String),
        task: j['task'] as String,
        model: j['model'] as String,
        result: j['result'] as String,
        source: j['source'] as String,
        attempt: (j['attempt'] as num?)?.toInt() ?? 1,
        chainPos: (j['chain_pos'] as num?)?.toInt() ?? 0,
        latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
      );
}

/// The answer of `GET /v1/llm`.
class LlmInfo {
  const LlmInfo({
    required this.tasks,
    required this.models,
    required this.quota,
    required this.maxFallback,
    required this.recentCalls,
    required this.warnings,
  });

  final Map<String, TaskModels> tasks;
  final List<ModelInfo> models;
  final Quota? quota;
  final int maxFallback;
  final List<LlmCall> recentCalls;
  final List<String> warnings;

  factory LlmInfo.fromJson(Map<String, dynamic> j) => LlmInfo(
        tasks: {
          for (final e in (j['tasks'] as Map).entries)
            e.key as String: TaskModels.fromJson(e.value as Map<String, dynamic>),
        },
        models: [
          for (final m in (j['models'] as List? ?? const [])) ModelInfo.fromJson(m as Map<String, dynamic>),
        ],
        quota: j['quota'] == null ? null : Quota.fromJson(j['quota'] as Map<String, dynamic>),
        maxFallback: (j['max_fallback'] as num?)?.toInt() ?? 3,
        recentCalls: [
          for (final c in (j['recent_calls'] as List? ?? const [])) LlmCall.fromJson(c as Map<String, dynamic>),
        ],
        warnings: [for (final w in (j['warnings'] as List? ?? const [])) w as String],
      );

  ModelInfo? model(String id) {
    for (final m in models) {
      if (m.id == id) return m;
    }
    return null;
  }
}
