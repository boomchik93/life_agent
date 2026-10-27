import 'models.dart';

/// One piece of a task in the plan, in wall-clock time of the user's zone.
class PlanChunk {
  const PlanChunk({
    required this.itemId,
    required this.title,
    required this.category,
    required this.start,
    required this.end,
  });

  final int itemId;
  final String title, category;
  final Wall start, end;

  factory PlanChunk.fromJson(Map<String, dynamic> j) => PlanChunk(
        itemId: (j['item_id'] as num).toInt(),
        title: j['title'] as String? ?? '',
        category: j['category'] as String? ?? 'other',
        start: Wall.parse(j['start'] as String),
        end: Wall.parse(j['end'] as String),
      );

  int get minutes => end.minutesSince(start);
}

/// A task that did not fit before its deadline.
class PlanUnplaced {
  const PlanUnplaced({required this.itemId, required this.title, required this.missingMin});

  final int itemId;
  final String title;
  final int missingMin;

  factory PlanUnplaced.fromJson(Map<String, dynamic> j) => PlanUnplaced(
        itemId: (j['item_id'] as num).toInt(),
        title: j['title'] as String? ?? '',
        missingMin: (j['missing_min'] as num).toInt(),
      );
}

/// A task due after this week: it waits for its own week.
class PlanDeferred {
  const PlanDeferred({required this.itemId, required this.title, required this.deadline});

  final int itemId;
  final String title;
  final String deadline; // YYYY-MM-DD

  factory PlanDeferred.fromJson(Map<String, dynamic> j) => PlanDeferred(
        itemId: (j['item_id'] as num).toInt(),
        title: j['title'] as String? ?? '',
        deadline: j['deadline'] as String? ?? '',
      );
}

/// A goal that was left out of the plan, and why.
class SkippedGoal {
  const SkippedGoal({required this.title, required this.reason});

  final String title, reason;
}

/// The answer of `POST /v1/plan/week`: a preview, nothing is written yet.
class PlanData {
  const PlanData({
    required this.from,
    required this.to,
    required this.placed,
    required this.unplaced,
    required this.deferred,
    required this.before,
    required this.after,
    required this.violations,
    this.skippedGoals = const [],
    this.goals = false,
  });

  /// Goals left out because of a red risk (only in a plan of the goals).
  final List<SkippedGoal> skippedGoals;

  /// The plan holds goal blocks: the server checks it against the goals too.
  final bool goals;

  final Wall from, to;
  final List<PlanChunk> placed;
  final List<PlanUnplaced> unplaced;
  final List<PlanDeferred> deferred;
  final Risks before, after;

  /// Rules the plan breaks. The server builds plans without them, so a
  /// non-empty list is a bug the user must not be able to apply.
  final List<String> violations;

  factory PlanData.fromJson(Map<String, dynamic> j) => PlanData(
        from: Wall.parse(j['from'] as String),
        to: Wall.parse(j['to'] as String),
        placed: [for (final c in (j['placed'] as List)) PlanChunk.fromJson(c as Map<String, dynamic>)],
        unplaced: [for (final u in (j['unplaced'] as List)) PlanUnplaced.fromJson(u as Map<String, dynamic>)],
        deferred: [for (final d in (j['deferred'] as List)) PlanDeferred.fromJson(d as Map<String, dynamic>)],
        before: Risks.fromJson(j['before'] as Map<String, dynamic>),
        after: Risks.fromJson(j['after'] as Map<String, dynamic>),
        violations: [for (final v in (j['violations'] as List? ?? const [])) v as String],
        skippedGoals: [
          for (final g in (j['skipped_goals'] as List? ?? const []))
            SkippedGoal(title: (g as Map)['title'] as String, reason: g['reason'] as String),
        ],
      );

  /// The same plan marked as a plan of goals (the request asked for them).
  PlanData withGoals(bool goals) => PlanData(
        from: from, to: to, placed: placed, unplaced: unplaced, deferred: deferred, before: before,
        after: after, violations: violations, skippedGoals: skippedGoals, goals: goals,
      );

  bool get isEmpty => placed.isEmpty && unplaced.isEmpty && deferred.isEmpty;

  /// The body of `POST /v1/plan/week/apply`: the same pieces the user saw.
  Map<String, dynamic> applyJson() => {
        if (goals) 'goals': true,
        'placed': [
          for (final c in placed)
            {
              'item_id': c.itemId,
              'start': _wall(c.start),
              'end': _wall(c.end),
            },
        ],
      };

  static String _wall(Wall w) => '${w.ymd}T${w.hm}';
}

class PlanApplied {
  const PlanApplied({required this.count, required this.duplicate});

  final int count;

  /// The request was already done before: nothing new was written.
  final bool duplicate;

  factory PlanApplied.fromJson(Map<String, dynamic> j) => PlanApplied(
        count: (j['applied'] as List).length,
        duplicate: j['duplicate'] as bool? ?? false,
      );
}
