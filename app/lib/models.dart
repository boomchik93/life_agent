/// A moment shown as wall-clock time in the user's time zone, the one the
/// server reports. The phone's own zone is deliberately ignored.
class Wall {
  const Wall(this.year, this.month, this.day, [this.hour = 0, this.minute = 0]);

  final int year, month, day, hour, minute;

  /// Parses RFC 3339 with an offset, e.g. `2026-10-27T18:00:00+05:00`.
  factory Wall.parse(String s) {
    final m = RegExp(r'^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d)').firstMatch(s);
    if (m == null) throw FormatException('bad time', s);
    return Wall(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!),
        int.parse(m[4]!), int.parse(m[5]!));
  }

  /// Day arithmetic and comparison go through a UTC DateTime, which has no DST.
  DateTime get _utc => DateTime.utc(year, month, day, hour, minute);

  Wall addDays(int n) {
    final d = DateTime.utc(year, month, day + n, hour, minute);
    return Wall(d.year, d.month, d.day, d.hour, d.minute);
  }

  Wall get dayStart => Wall(year, month, day);

  bool sameDay(Wall other) => year == other.year && month == other.month && day == other.day;

  bool isBefore(Wall other) => _utc.isBefore(other._utc);

  int get weekday => _utc.weekday; // 1 = Monday

  String get hm =>
      '${hour.toString().padLeft(2, '0')}:${minute.toString().padLeft(2, '0')}';

  String get ymd =>
      '$year-${month.toString().padLeft(2, '0')}-${day.toString().padLeft(2, '0')}';

  @override
  bool operator ==(Object other) => other is Wall && other._utc == _utc;

  @override
  int get hashCode => _utc.hashCode;
}

enum RiskColor {
  green,
  yellow,
  red,
  gray;

  static RiskColor parse(String s) => RiskColor.values.firstWhere(
        (c) => c.name == s,
        orElse: () => RiskColor.gray, // an unknown colour is never shown as healthy
      );
}

class Risk {
  const Risk({required this.color, required this.reasons, required this.metrics});

  final RiskColor color;
  final List<String> reasons;
  final Map<String, double> metrics;

  factory Risk.fromJson(Map<String, dynamic> j) => Risk(
        color: RiskColor.parse(j['color'] as String? ?? ''),
        reasons: [for (final r in (j['reasons'] as List? ?? const [])) r as String],
        metrics: {
          for (final e in ((j['metrics'] as Map?) ?? const {}).entries)
            e.key as String: (e.value as num).toDouble(),
        },
      );
}

class Risks {
  const Risks({
    required this.sleep,
    required this.overload,
    required this.noRest,
    required this.state,
  });

  final Risk sleep, overload, noRest, state;

  factory Risks.fromJson(Map<String, dynamic> j) => Risks(
        sleep: Risk.fromJson(j['sleep'] as Map<String, dynamic>),
        overload: Risk.fromJson(j['overload'] as Map<String, dynamic>),
        noRest: Risk.fromJson(j['no_rest'] as Map<String, dynamic>),
        state: Risk.fromJson(j['state'] as Map<String, dynamic>),
      );
}

class CalEvent {
  const CalEvent({
    required this.id,
    required this.source,
    required this.calendar,
    required this.title,
    required this.start,
    required this.end,
    required this.allDay,
  });

  final String id, source, calendar, title;
  final Wall start, end; // end is exclusive
  final bool allDay;

  factory CalEvent.fromJson(Map<String, dynamic> j) => CalEvent(
        id: j['id'] as String,
        source: j['source'] as String,
        calendar: j['calendar'] as String,
        title: j['title'] as String,
        start: Wall.parse(j['start'] as String),
        end: Wall.parse(j['end'] as String),
        allDay: j['all_day'] as bool,
      );

  /// Whether the event touches the given day.
  bool touches(Wall day) {
    final from = day.dayStart, to = day.dayStart.addDays(1);
    if (end == start) return !start.isBefore(from) && start.isBefore(to);
    return from.isBefore(end) && start.isBefore(to);
  }
}

class WeekData {
  const WeekData({
    required this.from,
    required this.to,
    required this.events,
    required this.warnings,
    required this.risks,
  });

  final Wall from, to; // [from, to), Monday to Monday
  final List<CalEvent> events;
  final List<String> warnings;
  final Risks risks;

  factory WeekData.fromJson(Map<String, dynamic> j) => WeekData(
        from: Wall.parse(j['from'] as String),
        to: Wall.parse(j['to'] as String),
        events: [
          for (final e in (j['events'] as List)) CalEvent.fromJson(e as Map<String, dynamic>),
        ],
        warnings: [for (final w in (j['warnings'] as List)) w as String],
        risks: Risks.fromJson(j['risks'] as Map<String, dynamic>),
      );

  List<Wall> get days {
    final out = <Wall>[];
    for (var d = from.dayStart; d.isBefore(to); d = d.addDays(1)) {
      out.add(d);
    }
    return out;
  }

  List<CalEvent> eventsOn(Wall day) => [
        for (final e in events)
          if (e.touches(day)) e,
      ];
}
