import 'package:life_agent/api.dart';
import 'package:life_agent/card.dart';
import 'package:life_agent/llm_info.dart';
import 'package:life_agent/models.dart';
import 'package:life_agent/plan.dart';
import 'package:life_agent/settings.dart';
import 'package:life_agent/slot.dart';

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

  final parsed = <String>[];
  Object? parseError;
  ParsedCard? parseResult;
  Duration parseDelay = Duration.zero;

  @override
  Future<ParsedCard> parse(String text) async {
    parsed.add(text);
    if (parseDelay > Duration.zero) await Future<void>.delayed(parseDelay);
    if (parseError != null) throw parseError!;
    return parseResult ?? sampleCard();
  }

  CheckResult checkResult = sampleCheck();
  Object? checkError;
  Object? writeError;
  final checked = <SlotRequest>[];
  final written = <({SlotRequest slot, String key})>[];
  final tasks = <({TaskRequest task, String key})>[];

  @override
  Future<CheckResult> check(SlotRequest slot) async {
    checked.add(slot);
    if (checkError != null) throw checkError!;
    return checkResult;
  }

  @override
  Future<Written> writeEvent(SlotRequest slot, {required String key}) async {
    if (writeError != null) throw writeError!;
    written.add((slot: slot, key: key));
    return const Written(id: 1, duplicate: false);
  }

  @override
  Future<Written> saveTask(TaskRequest task, {required String key}) async {
    if (writeError != null) throw writeError!;
    tasks.add((task: task, key: key));
    return const Written(id: 2, duplicate: false);
  }

  LlmInfo llm = sampleLlm();
  Object? llmError;
  Object? saveError;
  final saved = <Map<String, TaskModels>>[];

  @override
  Future<LlmInfo> llmInfo() async {
    if (llmError != null) throw llmError!;
    return llm;
  }

  @override
  Future<void> saveLlmTasks(Map<String, TaskModels> tasks) async {
    if (saveError != null) throw saveError!;
    saved.add(Map.of(tasks));
    llm = LlmInfo(
      tasks: Map.of(tasks),
      models: llm.models,
      quota: llm.quota,
      maxFallback: llm.maxFallback,
      recentCalls: llm.recentCalls,
      warnings: llm.warnings,
    );
  }

  PlanData planResult = samplePlan();
  Object? planError;
  Object? applyError;
  int plansBuilt = 0;
  final applied = <({PlanData plan, String key})>[];

  @override
  Future<PlanData> planWeek() async {
    plansBuilt++;
    if (planError != null) throw planError!;
    return planResult;
  }

  @override
  Future<PlanApplied> applyPlan(PlanData plan, {required String key}) async {
    if (applyError != null) throw applyError!;
    applied.add((plan: plan, key: key));
    return PlanApplied(count: plan.placed.length, duplicate: false);
  }

  @override
  Future<void> ping() async {
    if (weekError != null) throw weekError!;
  }
}

ParsedCard sampleCard({
  String kind = 'event',
  String title = 'КВН',
  String category = 'hobby',
  String? date = '2026-10-30',
  String? timeFrom = '18:00',
  String? timeTo = '20:00',
  int? durationMin,
  String? deadline,
  String? recurrence,
  List<String> missing = const [],
}) =>
    ParsedCard(
      kind: kind, title: title, category: category, date: date, timeFrom: timeFrom,
      timeTo: timeTo, durationMin: durationMin, deadline: deadline, recurrence: recurrence,
      missing: missing, model: 'vendor/model:free',
    );

LlmInfo sampleLlm({String parseModel = '', List<String> fallbacks = const []}) => LlmInfo(
      tasks: {
        'parse': TaskModels(
            model: parseModel, effort: 'low', fallbacks: fallbacks, reasoning: parseModel == 'vendor/think:free'),
        'advice': const TaskModels(model: '', effort: 'medium', fallbacks: [], reasoning: false),
      },
      models: const [
        ModelInfo(id: 'vendor/fast:free', name: 'Fast', free: true, reasoning: false, structured: true),
        ModelInfo(id: 'vendor/think:free', name: 'Think', free: true, reasoning: true, structured: true),
        ModelInfo(id: 'vendor/other:free', name: 'Other', free: true, reasoning: false, structured: false),
        ModelInfo(id: 'vendor/fast2:free', name: 'Fast2', free: true, reasoning: false, structured: true),
      ],
      quota: const Quota(used: 12, limit: 50, remaining: 38, resetAt: Wall(2026, 10, 29, 5)),
      maxFallback: 3,
      recentCalls: const [],
      warnings: const [],
    );

CheckResult sampleCheck({
  bool ok = true,
  bool needsConfirm = false,
  bool askCategory = false,
  String category = 'hobby',
  String source = 'user',
  List<String> reasons = const [],
  List<String> worsened = const [],
  List<Overlap> overlaps = const [],
  Risks? before,
  Risks? after,
}) {
  Risks green() => Risks(sleep: risk('gray'), overload: risk('green'), noRest: risk('green'), state: risk('gray'));
  return CheckResult(
    ok: ok, needsConfirm: needsConfirm, category: category, categorySource: source,
    askCategory: askCategory, overlaps: overlaps, reasons: reasons, worsened: worsened,
    before: before ?? green(), after: after ?? green(),
  );
}

PlanData samplePlan({
  List<PlanChunk>? placed,
  List<PlanUnplaced> unplaced = const [],
  List<PlanDeferred> deferred = const [],
  List<String> violations = const [],
  Risks? before,
  Risks? after,
}) {
  Risks g(String overload) => Risks(sleep: risk('gray'), overload: risk(overload), noRest: risk('green'), state: risk('gray'));
  return PlanData(
    from: const Wall(2026, 10, 26),
    to: const Wall(2026, 11, 2),
    placed: placed ??
        const [
          PlanChunk(itemId: 7, title: 'Курсовая', category: 'study', start: Wall(2026, 10, 28, 13), end: Wall(2026, 10, 28, 15)),
          PlanChunk(itemId: 7, title: 'Курсовая', category: 'study', start: Wall(2026, 10, 28, 15, 15), end: Wall(2026, 10, 28, 17, 15)),
          PlanChunk(itemId: 7, title: 'Курсовая', category: 'study', start: Wall(2026, 10, 29, 9), end: Wall(2026, 10, 29, 11)),
        ],
    unplaced: unplaced,
    deferred: deferred,
    before: before ?? g('green'),
    after: after ?? g('yellow'),
    violations: violations,
  );
}
