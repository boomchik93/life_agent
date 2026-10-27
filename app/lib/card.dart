import 'models.dart';

const categoryNames = {
  'study': 'Учёба',
  'work': 'Работа',
  'hobby': 'Хобби',
  'rest': 'Отдых',
  'chores': 'Дела',
  'other': 'Другое',
};

const _missingNames = {
  'date': 'дата',
  'time_from': 'время начала',
  'time_to': 'время конца',
  'duration_min': 'длительность',
  'deadline': 'срок',
};

/// The answer of `POST /v1/parse`: the parts of the phrase with the dates
/// already calculated by the server.
class ParsedCard {
  const ParsedCard({
    required this.kind,
    required this.title,
    required this.category,
    this.date,
    this.timeFrom,
    this.timeTo,
    this.durationMin,
    this.deadline,
    this.recurrence,
    this.missing = const [],
    this.model = '',
  });

  final String kind; // event or task
  final String title, category;
  final String? date, timeFrom, timeTo, deadline, recurrence; // YYYY-MM-DD, HH:MM, RRULE
  final int? durationMin;
  final List<String> missing;
  final String model;

  factory ParsedCard.fromJson(Map<String, dynamic> j) => ParsedCard(
        kind: j['kind'] as String,
        title: j['title'] as String? ?? '',
        category: j['category_guess'] as String? ?? 'other',
        date: j['date'] as String?,
        timeFrom: j['time_from'] as String?,
        timeTo: j['time_to'] as String?,
        durationMin: (j['duration_min'] as num?)?.toInt(),
        deadline: j['deadline'] as String?,
        recurrence: j['recurrence'] as String?,
        missing: [for (final m in (j['missing'] as List? ?? const [])) m as String],
        model: j['model'] as String? ?? '',
      );
}

Wall? _parseDay(String? s) {
  if (s == null) return null;
  final m = RegExp(r'^(\d{4})-(\d\d)-(\d\d)$').firstMatch(s);
  return m == null ? null : Wall(int.parse(m[1]!), int.parse(m[2]!), int.parse(m[3]!));
}

/// Minutes since midnight from "HH:MM".
int? _parseMinutes(String? s) {
  if (s == null) return null;
  final m = RegExp(r'^(\d{1,2}):(\d\d)$').firstMatch(s);
  if (m == null) return null;
  final h = int.parse(m[1]!), min = int.parse(m[2]!);
  return h < 24 && min < 60 ? h * 60 + min : null;
}

/// The card as the user edits it. Pure data and rules, no widgets; the dates
/// are checked here again after every edit, like the server does on parse.
class CardDraft {
  CardDraft({
    required this.kind,
    required this.title,
    required this.category,
    this.date,
    this.from,
    this.to,
    this.durationMin,
    this.deadline,
    this.recurrence,
  });

  String kind; // event or task
  String title;
  String category;
  Wall? date; // event day
  int? from, to; // minutes since midnight
  int? durationMin; // task
  Wall? deadline;
  String? recurrence;

  factory CardDraft.fromParsed(ParsedCard c) {
    final from = _parseMinutes(c.timeFrom);
    var to = _parseMinutes(c.timeTo);
    // «Занятие на 2 часа с 18:00»: the end follows from the duration.
    if (c.kind == 'event' && to == null && from != null && c.durationMin != null) {
      final end = from + c.durationMin!;
      if (end < 24 * 60) to = end;
    }
    return CardDraft(
      kind: c.kind,
      title: c.title,
      category: categoryNames.containsKey(c.category) ? c.category : 'other',
      date: _parseDay(c.date),
      from: from,
      to: to,
      durationMin: c.kind == 'task' ? c.durationMin : null,
      deadline: _parseDay(c.deadline),
      recurrence: c.recurrence,
    );
  }

  bool get isEvent => kind == 'event';

  /// Event length in minutes when both ends are set and in order.
  int? get eventMinutes => (from != null && to != null && to! > from!) ? to! - from! : null;

  /// Names of the fields that are still empty, in the order of the screen.
  List<String> missing() => isEvent
      ? [if (date == null) 'date', if (from == null) 'time_from', if (to == null) 'time_to']
      : [if (durationMin == null) 'duration_min', if (deadline == null) 'deadline'];

  /// Checks that need the current moment [now]. A problem is a warning with the
  /// reason in words; the user decides whether to go on.
  List<String> problems(Wall now) {
    final out = <String>[];
    if (isEvent) {
      if (from != null && to != null && to! <= from!) out.add('Конец события не позже начала.');
      final d = date;
      if (d != null && from != null) {
        final start = Wall(d.year, d.month, d.day, from! ~/ 60, from! % 60);
        if (start.isBefore(now)) out.add('Это время уже прошло.');
      }
    } else {
      final d = deadline;
      if (d != null) {
        final today = now.dayStart;
        if (d.isBefore(today)) {
          out.add('Срок уже прошёл.');
        } else if (durationMin != null) {
          final left = d.addDays(1).minutesSince(now);
          if (left < durationMin!) out.add('До срока осталось меньше времени, чем нужно на задачу.');
        }
      }
    }
    return out;
  }

  /// Ready to be saved: the title and every field are filled and consistent.
  bool get isComplete {
    if (title.trim().isEmpty || missing().isNotEmpty) return false;
    return !(isEvent && to! <= from!);
  }
}

String missingText(List<String> missing) =>
    missing.map((m) => _missingNames[m] ?? m).join(', ');

String minutesHm(int m) =>
    '${(m ~/ 60).toString().padLeft(2, '0')}:${(m % 60).toString().padLeft(2, '0')}';

/// "6 ч", "1 ч 30 мин", "45 мин".
String durationText(int minutes) {
  final h = minutes ~/ 60, m = minutes % 60;
  if (h == 0) return '$m мин';
  return m == 0 ? '$h ч' : '$h ч $m мин';
}

const _dayNames = {
  'MO': 'понедельникам', 'TU': 'вторникам', 'WE': 'средам', 'TH': 'четвергам',
  'FR': 'пятницам', 'SA': 'субботам', 'SU': 'воскресеньям',
};

/// Short words for the simple rules the parser produces; anything else is shown as is.
String recurrenceText(String rule) {
  final parts = {
    for (final p in rule.split(';'))
      if (p.contains('=')) p.split('=')[0].toUpperCase(): p.split('=')[1].toUpperCase(),
  };
  final byday = parts['BYDAY']?.split(',').map((d) => _dayNames[d]).toList();
  switch (parts['FREQ']) {
    case 'DAILY':
      return 'каждый день';
    case 'WEEKLY':
      if (byday != null && !byday.contains(null)) return 'по ${byday.join(', ')}';
      return 'каждую неделю';
    case 'MONTHLY':
      return 'каждый месяц';
  }
  return rule;
}
