import 'dart:math';

import 'card.dart';
import 'models.dart';

/// An event as `POST /v1/check` and `POST /v1/events` take it. Times are
/// wall-clock in the user's zone, the server adds the offset.
class SlotRequest {
  const SlotRequest({
    required this.title,
    required this.start,
    required this.end,
    required this.category,
    this.recurrence,
    this.categoryConfirmed = false,
    this.confirmed = false,
  });

  final String title, category;
  final Wall start, end;
  final String? recurrence;

  /// The user answered "is this study?" or picked the category.
  final bool categoryConfirmed;

  /// The user saw the warnings and wants the event anyway.
  final bool confirmed;

  SlotRequest copyWith({String? category, bool? categoryConfirmed, bool? confirmed}) => SlotRequest(
        title: title,
        start: start,
        end: end,
        category: category ?? this.category,
        recurrence: recurrence,
        categoryConfirmed: categoryConfirmed ?? this.categoryConfirmed,
        confirmed: confirmed ?? this.confirmed,
      );

  Map<String, dynamic> toJson() => {
        'title': title.trim(),
        'start': '${start.ymd}T${start.hm}',
        'end': '${end.ymd}T${end.hm}',
        'category': category,
        'category_confirmed': categoryConfirmed,
        'confirmed': confirmed,
        if (recurrence != null) 'recurrence': recurrence,
      };
}

/// A task with a deadline for `POST /v1/items`.
class TaskRequest {
  const TaskRequest({
    required this.title,
    required this.category,
    required this.durationMin,
    required this.deadline,
  });

  final String title, category;
  final int durationMin;
  final Wall deadline;

  Map<String, dynamic> toJson() => {
        'title': title.trim(),
        'category': category,
        'category_confirmed': true, // the card shows the category and the user saved it
        'duration_min': durationMin,
        'deadline': deadline.ymd,
      };
}

extension CardDraftRequests on CardDraft {
  /// The event of a complete draft.
  SlotRequest toSlot() => SlotRequest(
        title: title,
        start: Wall(date!.year, date!.month, date!.day, from! ~/ 60, from! % 60),
        end: Wall(date!.year, date!.month, date!.day, to! ~/ 60, to! % 60),
        category: category,
        recurrence: recurrence,
      );

  TaskRequest toTask() =>
      TaskRequest(title: title, category: category, durationMin: durationMin!, deadline: deadline!);
}

class Overlap {
  const Overlap({required this.title, required this.start, required this.end});

  final String title;
  final Wall start, end;

  factory Overlap.fromJson(Map<String, dynamic> j) => Overlap(
        title: j['title'] as String,
        start: Wall.parse(j['start'] as String),
        end: Wall.parse(j['end'] as String),
      );
}

/// The answer of `POST /v1/check`: can the event be added, and why.
class CheckResult {
  const CheckResult({
    required this.ok,
    required this.needsConfirm,
    required this.category,
    required this.categorySource,
    required this.askCategory,
    required this.overlaps,
    required this.reasons,
    required this.worsened,
    required this.before,
    required this.after,
    this.recurrenceNote,
  });

  final bool ok, needsConfirm, askCategory;
  final String category;
  final String categorySource; // memory, user or guess
  final List<Overlap> overlaps;
  final List<String> reasons;
  final List<String> worsened; // sleep, overload, no_rest, state
  final Risks before, after;
  final String? recurrenceNote;

  factory CheckResult.fromJson(Map<String, dynamic> j) => CheckResult(
        ok: j['ok'] as bool,
        needsConfirm: j['needs_confirm'] as bool,
        category: j['category'] as String,
        categorySource: j['category_source'] as String? ?? 'guess',
        askCategory: j['ask_category'] as bool,
        overlaps: [for (final o in (j['overlaps'] as List)) Overlap.fromJson(o as Map<String, dynamic>)],
        reasons: [for (final r in (j['reasons'] as List)) r as String],
        worsened: [for (final r in (j['worsened'] as List)) r as String],
        before: Risks.fromJson(j['before'] as Map<String, dynamic>),
        after: Risks.fromJson(j['after'] as Map<String, dynamic>),
        recurrenceNote: j['recurrence_note'] as String?,
      );
}

/// What the server saved (`POST /v1/events`, `POST /v1/items`).
class Written {
  const Written({required this.id, required this.duplicate});

  final int id;

  /// The request was already done before: nothing new was made.
  final bool duplicate;

  factory Written.fromJson(Map<String, dynamic> j) => Written(
        id: ((j['item'] as Map)['id'] as num).toInt(),
        duplicate: j['duplicate'] as bool? ?? false,
      );
}

/// The server asked for the user's OK before writing.
class NeedsConfirm implements Exception {
  const NeedsConfirm(this.check);

  final CheckResult check;
}

/// One idempotency key per payload: a retry or a double tap of the same
/// request repeats the key, a changed request gets a new one.
class IdempotencyKeys {
  IdempotencyKeys([Random? random]) : _random = random ?? Random.secure();

  final Random _random;
  String? _payload, _key;

  String forPayload(String payload) {
    if (payload != _payload || _key == null) {
      _payload = payload;
      _key = [for (var i = 0; i < 16; i++) _random.nextInt(256).toRadixString(16).padLeft(2, '0')].join();
    }
    return _key!;
  }
}
