import 'models.dart';

/// A goal with its weekly norm and the norm the server proposes, if any.
class GoalInfo {
  const GoalInfo({required this.goal, required this.title, this.normMin, this.suggestedMin});

  final String goal, title;
  final int? normMin, suggestedMin;

  factory GoalInfo.fromJson(Map<String, dynamic> j) => GoalInfo(
        goal: j['goal'] as String,
        title: j['title'] as String? ?? j['goal'] as String,
        normMin: (j['weekly_norm_min'] as num?)?.toInt(),
        suggestedMin: (j['suggested_min'] as num?)?.toInt(),
      );
}

class Goals {
  const Goals({required this.goals, required this.dataDays, required this.suggestAfterDays});

  final List<GoalInfo> goals;
  final int dataDays, suggestAfterDays;

  factory Goals.fromJson(Map<String, dynamic> j) => Goals(
        goals: [for (final g in (j['goals'] as List)) GoalInfo.fromJson(g as Map<String, dynamic>)],
        dataDays: (j['data_days'] as num?)?.toInt() ?? 0,
        suggestAfterDays: (j['suggest_after_days'] as num?)?.toInt() ?? 14,
      );
}

class ReviewGoal {
  const ReviewGoal({required this.goal, required this.title, required this.factMin, this.normMin, this.percent});

  final String goal, title;
  final int factMin;
  final int? normMin, percent;

  factory ReviewGoal.fromJson(Map<String, dynamic> j) => ReviewGoal(
        goal: j['goal'] as String,
        title: j['title'] as String,
        factMin: (j['fact_min'] as num).toInt(),
        normMin: (j['norm_min'] as num?)?.toInt(),
        percent: (j['percent'] as num?)?.toInt(),
      );
}

class ReviewGap {
  const ReviewGap({required this.title, required this.missingMin});

  final String title;
  final int missingMin;
}

/// The weekly review as the server calculated it (`GET /v1/review/week`).
class Review {
  const Review({
    required this.weekStart,
    required this.weekEnd,
    required this.goals,
    required this.leisureMin,
    required this.socialMin,
    required this.sleepMin,
    required this.sleepDays,
    required this.unknownPercent,
    required this.nightMin,
    required this.contributions,
    required this.gaps,
    required this.balanceMin,
    required this.balanceSpread,
    required this.warnings,
    required this.text,
  });

  final Wall weekStart, weekEnd;
  final List<ReviewGoal> goals;
  final int leisureMin, socialMin, sleepMin, sleepDays, unknownPercent, nightMin, contributions;
  final List<ReviewGap> gaps;
  final int? balanceMin, balanceSpread;
  final List<String> warnings, text;

  factory Review.fromJson(Map<String, dynamic> j) {
    final b = j['balance'] as Map<String, dynamic>?;
    return Review(
      weekStart: Wall.parse('${j['week_start']}T00:00'),
      weekEnd: Wall.parse('${j['week_end']}T00:00'),
      goals: [for (final g in (j['goals'] as List)) ReviewGoal.fromJson(g as Map<String, dynamic>)],
      leisureMin: (j['leisure_min'] as num).toInt(),
      socialMin: (j['social_min'] as num).toInt(),
      sleepMin: (j['sleep_min'] as num).toInt(),
      sleepDays: (j['sleep_days'] as num).toInt(),
      unknownPercent: (j['unknown_percent'] as num).toInt(),
      nightMin: (j['night_min'] as num).toInt(),
      contributions: (j['contributions'] as num).toInt(),
      gaps: [
        for (final g in (j['gaps'] as List))
          ReviewGap(title: (g as Map)['title'] as String, missingMin: (g['missing_min'] as num).toInt()),
      ],
      balanceMin: (b?['min_percent'] as num?)?.toInt(),
      balanceSpread: (b?['spread_percent'] as num?)?.toInt(),
      warnings: [for (final w in (j['warnings'] as List)) w as String],
      text: [for (final t in (j['text'] as List)) t as String],
    );
  }
}
