import 'dart:io';

import 'package:health/health.dart';

import 'api.dart';

enum SampleKind { sleepStage, sleepSession, heartRate }

/// One reading from Health Connect, reduced to what the summary needs.
class Sample {
  const Sample(this.kind, this.from, this.to, [this.value = 0]);

  final SampleKind kind;
  final DateTime from, to;
  final double value;
}

/// The summary of one day: the only thing that leaves the phone.
class HealthDay {
  const HealthDay({required this.date, this.sleepHours, this.avgHr});

  final String date; // YYYY-MM-DD
  final double? sleepHours;
  final double? avgHr;

  bool get isEmpty => sleepHours == null && avgHr == null;

  Map<String, dynamic> toJson() => {'date': date, 'sleep_hours': ?sleepHours, 'avg_hr': ?avgHr};
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
HealthDay summarizeDay(DateTime day, List<Sample> samples) {
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
  return HealthDay(date: _ymd(day), sleepHours: sleep, avgHr: avg);
}

enum HealthStatus { unsupported, notInstalled, needsAccess, ready }

/// Where the readings come from. Health Connect on the phone; a fake in tests.
abstract class HealthSource {
  Future<HealthStatus> status();

  /// Asks for the read permissions. Returns whether they are granted now.
  Future<bool> requestAccess();

  /// Whether the app may read while it is closed (needed by the background task).
  Future<bool> backgroundAllowed();

  /// Sends the user to install Health Connect.
  Future<void> install();

  Future<List<Sample>> read(DateTime from, DateTime to);
}

class HealthConnectSource implements HealthSource {
  HealthConnectSource([Health? health]) : _h = health ?? Health();

  final Health _h;
  bool _configured = false;

  static const _types = [
    HealthDataType.SLEEP_SESSION,
    HealthDataType.SLEEP_LIGHT,
    HealthDataType.SLEEP_DEEP,
    HealthDataType.SLEEP_REM,
    HealthDataType.SLEEP_ASLEEP,
    HealthDataType.HEART_RATE,
  ];

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
    final granted = await _h.hasPermissions(_types) ?? false;
    return granted ? HealthStatus.ready : HealthStatus.needsAccess;
  }

  @override
  Future<bool> requestAccess() async {
    await _configure();
    final ok = await _h.requestAuthorization(_types);
    if (ok && await _h.isHealthDataInBackgroundAvailable()) {
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
    final points = await _h.getHealthDataFromTypes(types: _types, startTime: from, endTime: to);
    final out = <Sample>[];
    for (final p in points) {
      final v = p.value;
      final num value = v is NumericHealthValue ? v.numericValue : 0;
      final kind = switch (p.type) {
        HealthDataType.SLEEP_SESSION => SampleKind.sleepSession,
        HealthDataType.HEART_RATE => SampleKind.heartRate,
        _ => SampleKind.sleepStage,
      };
      out.add(Sample(kind, p.dateFrom.toLocal(), p.dateTo.toLocal(), value.toDouble()));
    }
    return out;
  }
}

class SyncResult {
  const SyncResult({this.sent = 0, this.skipped});

  /// Days sent to the server.
  final int sent;

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
    var sent = 0;
    for (final day in [DateTime(today.year, today.month, today.day - 1), today]) {
      final summary = summarizeDay(day, samples);
      if (summary.isEmpty) continue;
      await api.sendHealthDay(summary);
      sent++;
    }
    return SyncResult(sent: sent, skipped: sent == 0 ? 'В Health Connect нет сна и пульса за эти дни.' : null);
  }
}
