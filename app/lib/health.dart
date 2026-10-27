import 'dart:io';

import 'package:health/health.dart';

import 'api.dart';

enum SampleKind { sleepStage, sleepSession, heartRate, restingHeartRate, workout }

/// One reading from Health Connect, reduced to what the summary needs.
class Sample {
  const Sample(this.kind, this.from, this.to, [this.value = 0]);

  final SampleKind kind;
  final DateTime from, to;
  final double value;
}

/// The summary of one day: the only thing that leaves the phone.
class HealthDay {
  const HealthDay({required this.date, this.sleepHours, this.avgHr, this.restingHr, this.steps, this.workoutMin});

  final String date; // YYYY-MM-DD
  final double? sleepHours;
  final double? avgHr;
  final double? restingHr;
  final int? steps;

  /// Minutes of recorded workouts; they count as sport.
  final int? workoutMin;

  bool get isEmpty =>
      sleepHours == null && avgHr == null && restingHr == null && steps == null && workoutMin == null;

  Map<String, dynamic> toJson() => {
        'date': date,
        'sleep_hours': ?sleepHours,
        'avg_hr': ?avgHr,
        'resting_hr': ?restingHr,
        'steps': ?steps,
        'workout_min': ?workoutMin,
      };
}

String _ymd(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

bool _sameDay(DateTime a, DateTime b) => a.year == b.year && a.month == b.month && a.day == b.day;

/// Total length of the union of intervals, so overlapping stages count once.
Duration _union(List<(DateTime, DateTime)> spans) {
  if (spans.isEmpty) return Duration.zero;
  final s = [...spans]..sort((a, b) => a.$1.compareTo(b.$1));
  var total = Duration.zero;
  var curFrom = s.first.$1, curTo = s.first.$2;
  for (final p in s.skip(1)) {
    if (p.$1.isAfter(curTo)) {
      total += curTo.difference(curFrom);
      curFrom = p.$1;
      curTo = p.$2;
    } else if (p.$2.isAfter(curTo)) {
      curTo = p.$2;
    }
  }
  return total + curTo.difference(curFrom);
}

double _round(double v, int digits) {
  final f = digits == 1 ? 10 : 100;
  return (v * f).round() / f;
}

/// Builds the summary of [day] (the day the user woke up on).
///
/// Sleep is the night that ended on [day] and began after 18:00 the day
/// before. Sleep stages are preferred; a bare session counts only when the
/// watch gave no stages. Heart rate is the mean of the day's readings.
HealthDay summarizeDay(DateTime day, List<Sample> samples, {int? steps}) {
  final nightFrom = DateTime(day.year, day.month, day.day - 1, 18);
  bool night(Sample s) => _sameDay(s.to, day) && !s.from.isBefore(nightFrom) && s.to.isAfter(s.from);

  var spans = [for (final s in samples) if (s.kind == SampleKind.sleepStage && night(s)) (s.from, s.to)];
  if (spans.isEmpty) {
    spans = [for (final s in samples) if (s.kind == SampleKind.sleepSession && night(s)) (s.from, s.to)];
  }
  final minutes = _union(spans).inMinutes;
  final sleep = minutes > 0 && minutes <= 24 * 60 ? _round(minutes / 60, 2) : null;

  final hr = [
    for (final s in samples)
      if (s.kind == SampleKind.heartRate && _sameDay(s.from, day) && s.value >= 20 && s.value <= 250) s.value,
  ];
  final avg = hr.isEmpty ? null : _round(hr.reduce((a, b) => a + b) / hr.length, 1);

  final resting = [
    for (final s in samples)
      if (s.kind == SampleKind.restingHeartRate && _sameDay(s.from, day) && s.value >= 20 && s.value <= 200) s.value,
  ];
  final restingAvg = resting.isEmpty ? null : _round(resting.reduce((a, b) => a + b) / resting.length, 1);

  // Workouts that began on the day; overlapping records (phone and watch) count once.
  final workout = _union([
    for (final s in samples)
      if (s.kind == SampleKind.workout && _sameDay(s.from, day) && s.to.isAfter(s.from)) (s.from, s.to),
  ]).inMinutes;

  return HealthDay(
    date: _ymd(day),
    sleepHours: sleep,
    avgHr: avg,
    restingHr: restingAvg,
    steps: steps != null && steps > 0 ? steps : null,
    workoutMin: workout > 0 && workout <= 24 * 60 ? workout : null,
  );
}

enum HealthStatus { unsupported, notInstalled, needsAccess, ready }

/// The kinds of readings, each with its own Health Connect permission. Any of
/// them is enough to send a summary: a refused one only leaves its field out.
enum HealthPart { sleep, heartRate, restingHeartRate, workout, steps }

const healthPartNames = {
  HealthPart.sleep: 'Сон',
  HealthPart.heartRate: 'Пульс',
  HealthPart.restingHeartRate: 'Пульс покоя',
  HealthPart.workout: 'Тренировки',
  HealthPart.steps: 'Шаги',
};

/// Where the readings come from. Health Connect on the phone; a fake in tests.
abstract class HealthSource {
  /// Ready when at least one kind of reading is granted.
  Future<HealthStatus> status();

  /// The kinds of readings the app may read now.
  Future<Set<HealthPart>> granted();

  /// Asks for the read permissions. Returns whether any is granted now.
  Future<bool> requestAccess();

  /// Whether the app may read while it is closed (needed by the background task).
  Future<bool> backgroundAllowed();

  /// Sends the user to install Health Connect.
  Future<void> install();

  Future<List<Sample>> read(DateTime from, DateTime to);

  /// Steps in the period, counted once even if the phone and the watch both
  /// record them (Health Connect merges the sources). Null when unknown.
  Future<int?> steps(DateTime from, DateTime to);

  /// Asks for the heart-rate-variability permission and reports how many of
  /// the last 7 days have such readings. Nothing is sent anywhere: this tells
  /// the user whether the watch gives HRV at all.
  Future<int> hrvDays(DateTime now);
}

class HealthConnectSource implements HealthSource {
  HealthConnectSource([Health? health]) : _h = health ?? Health();

  final Health _h;
  bool _configured = false;

  static const _parts = {
    HealthPart.sleep: [
      HealthDataType.SLEEP_SESSION,
      HealthDataType.SLEEP_LIGHT,
      HealthDataType.SLEEP_DEEP,
      HealthDataType.SLEEP_REM,
      HealthDataType.SLEEP_ASLEEP,
    ],
    HealthPart.heartRate: [HealthDataType.HEART_RATE],
    HealthPart.restingHeartRate: [HealthDataType.RESTING_HEART_RATE],
    HealthPart.workout: [HealthDataType.WORKOUT],
    HealthPart.steps: [HealthDataType.STEPS],
  };

  Future<void> _configure() async {
    if (_configured) return;
    await _h.configure();
    _configured = true;
  }

  @override
  Future<HealthStatus> status() async {
    if (!Platform.isAndroid) return HealthStatus.unsupported;
    await _configure();
    if (!await _h.isHealthConnectAvailable()) return HealthStatus.notInstalled;
    return (await granted()).isEmpty ? HealthStatus.needsAccess : HealthStatus.ready;
  }

  @override
  Future<Set<HealthPart>> granted() async {
    await _configure();
    final out = <HealthPart>{};
    for (final e in _parts.entries) {
      if (await _h.hasPermissions(e.value) ?? false) out.add(e.key);
    }
    return out;
  }

  @override
  Future<bool> requestAccess() async {
    await _configure();
    // One dialog for everything; the user may switch off any kind there and
    // the rest still works.
    await _h.requestAuthorization([for (final t in _parts.values) ...t]);
    final ok = (await granted()).isNotEmpty;
    if (ok && await _h.isHealthDataInBackgroundAvailable() && !await _h.isHealthDataInBackgroundAuthorized()) {
      await _h.requestHealthDataInBackgroundAuthorization();
    }
    return ok;
  }

  @override
  Future<bool> backgroundAllowed() async {
    await _configure();
    return await _h.isHealthDataInBackgroundAvailable() && await _h.isHealthDataInBackgroundAuthorized();
  }

  @override
  Future<void> install() async => _h.installHealthConnect();

  @override
  Future<List<Sample>> read(DateTime from, DateTime to) async {
    await _configure();
    final have = await granted();
    final types = [
      for (final e in _parts.entries)
        if (e.key != HealthPart.steps && have.contains(e.key)) ...e.value,
    ];
    if (types.isEmpty) return const [];
    final points = await _h.getHealthDataFromTypes(types: types, startTime: from, endTime: to);
    final out = <Sample>[];
    for (final p in points) {
      final v = p.value;
      final num value = v is NumericHealthValue ? v.numericValue : 0;
      final kind = switch (p.type) {
        HealthDataType.SLEEP_SESSION => SampleKind.sleepSession,
        HealthDataType.HEART_RATE => SampleKind.heartRate,
        HealthDataType.RESTING_HEART_RATE => SampleKind.restingHeartRate,
        HealthDataType.WORKOUT => SampleKind.workout,
        _ => SampleKind.sleepStage,
      };
      out.add(Sample(kind, p.dateFrom.toLocal(), p.dateTo.toLocal(), value.toDouble()));
    }
    return out;
  }

  @override
  Future<int?> steps(DateTime from, DateTime to) async {
    await _configure();
    try {
      if (!(await _h.hasPermissions(_parts[HealthPart.steps]!) ?? false)) return null;
      return await _h.getTotalStepsInInterval(from, to);
    } catch (_) {
      return null;
    }
  }

  @override
  Future<int> hrvDays(DateTime now) async {
    await _configure();
    const types = [HealthDataType.HEART_RATE_VARIABILITY_RMSSD];
    if (!(await _h.hasPermissions(types) ?? false) && !await _h.requestAuthorization(types)) return 0;
    final points = await _h.getHealthDataFromTypes(
      types: types,
      startTime: now.subtract(const Duration(days: 7)),
      endTime: now,
    );
    return {for (final p in points) _ymd(p.dateFrom.toLocal())}.length;
  }
}

/// What a summary holds, in words: «сон 7.5 ч, пульс 64, шаги 8000».
String describeDay(HealthDay d) {
  String n(double v) => v == v.roundToDouble() ? v.toInt().toString() : v.toString();
  return [
    if (d.sleepHours != null) 'сон ${n(d.sleepHours!)} ч',
    if (d.avgHr != null) 'пульс ${n(d.avgHr!)}',
    if (d.restingHr != null) 'пульс покоя ${n(d.restingHr!)}',
    if (d.steps != null) 'шаги ${d.steps}',
    if (d.workoutMin != null) 'тренировки ${d.workoutMin} мин',
  ].join(', ');
}

class SyncResult {
  const SyncResult({this.days = const [], this.skipped});

  /// The summaries sent to the server.
  final List<HealthDay> days;

  /// Days sent to the server.
  int get sent => days.length;

  /// What was sent, day by day: «27.10: сон 7 ч, пульс 64; 28.10: шаги 3000».
  String get summary =>
      [for (final d in days) '${d.date.substring(8)}.${d.date.substring(5, 7)}: ${describeDay(d)}'].join('; ');

  /// Why nothing was sent: no Health Connect, no permission, no readings.
  final String? skipped;
}

/// Reads yesterday's and today's readings and sends the two summaries.
class HealthSync {
  HealthSync({required this.api, required this.source, DateTime Function()? now}) : _now = now ?? DateTime.now;

  final LifeApi api;
  final HealthSource source;
  final DateTime Function() _now;

  Future<SyncResult> run() async {
    switch (await source.status()) {
      case HealthStatus.unsupported:
        return const SyncResult(skipped: 'Health Connect есть только на Android.');
      case HealthStatus.notInstalled:
        return const SyncResult(skipped: 'Health Connect не установлен.');
      case HealthStatus.needsAccess:
        return const SyncResult(skipped: 'Нет доступа к данным здоровья.');
      case HealthStatus.ready:
    }
    final now = _now();
    final today = DateTime(now.year, now.month, now.day);
    final samples = await source.read(DateTime(today.year, today.month, today.day - 2, 18), now);
    final sent = <HealthDay>[];
    for (final day in [DateTime(today.year, today.month, today.day - 1), today]) {
      final end = DateTime(day.year, day.month, day.day + 1);
      final steps = await source.steps(day, end.isAfter(now) ? now : end);
      final summary = summarizeDay(day, samples, steps: steps);
      if (summary.isEmpty) continue;
      await api.sendHealthDay(summary);
      sent.add(summary);
    }
    return SyncResult(days: sent, skipped: sent.isEmpty ? 'В Health Connect нет данных за вчера и сегодня.' : null);
  }
}
