import 'package:life_agent/api.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/settings.dart';

class MemoryStorage implements AppStorage {
  MemoryStorage({this.connection = const ConnectionSettings(baseUrl: '', token: ''), this.checkinDate});

  ConnectionSettings connection;
  String? checkinDate;

  @override
  Future<ConnectionSettings> loadConnection() async => connection;
  @override
  Future<void> saveConnection(ConnectionSettings c) async => connection = c;
  @override
  Future<String?> lastCheckinDate() async => checkinDate;
  @override
  Future<void> saveCheckinDate(String ymd) async => checkinDate = ymd;
}

Risk risk(String color, {List<String> reasons = const [], Map<String, double> metrics = const {}}) =>
    Risk(color: RiskColor.parse(color), reasons: reasons, metrics: metrics);

WeekData sampleWeek({
  Risk? sleep,
  Risk? overload,
  Risk? noRest,
  Risk? state,
  List<String> warnings = const [],
  List<CalEvent>? events,
}) =>
    WeekData(
      from: const Wall(2026, 10, 26),
      to: const Wall(2026, 11, 2),
      warnings: warnings,
      events: events ??
          const [
            CalEvent(
              id: '1', source: 'google', calendar: 'КВН', title: 'КВН',
              start: Wall(2026, 10, 30, 18), end: Wall(2026, 10, 30, 20), allDay: false,
            ),
          ],
      risks: Risks(
        sleep: sleep ?? risk('gray'),
        overload: overload ?? risk('green'),
        noRest: noRest ?? risk('green'),
        state: state ?? risk('gray'),
      ),
    );

class FakeApi implements LifeApi {
  FakeApi({WeekData? week}) : _week = week ?? sampleWeek();

  WeekData _week;
  final calls = <String>[];
  final checkins = <({int energy, int mood, double? sleep})>[];
  Object? weekError;
  Object? checkinError;

  void setWeek(WeekData w) => _week = w;

  @override
  Future<WeekData> week({Wall? start}) async {
    calls.add('week ${start?.ymd}');
    if (weekError != null) throw weekError!;
    if (start == null) return _week;
    // Like the server: the week that starts at (or contains) [start]; tests pass Mondays.
    return WeekData(
      from: start, to: start.addDays(7), events: const [], warnings: _week.warnings, risks: _week.risks);
  }

  @override
  Future<void> checkin({required int energy, required int mood, double? sleepHours}) async {
    if (checkinError != null) throw checkinError!;
    checkins.add((energy: energy, mood: mood, sleep: sleepHours));
  }

  @override
  Future<void> ping() async {
    if (weekError != null) throw weekError!;
  }
}
