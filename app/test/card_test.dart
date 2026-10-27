import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/card.dart';
import 'package:life_agent/models.dart';

import 'fakes.dart';

const now = Wall(2026, 10, 28, 12); // Wednesday

void main() {
  test('ParsedCard reads the server answer', () {
    final c = ParsedCard.fromJson({
      'kind': 'task', 'title': 'Курсовая', 'category_guess': 'study', 'date': null,
      'time_from': null, 'time_to': null, 'start': null, 'end': null, 'duration_min': 360,
      'deadline': '2026-10-30', 'recurrence': null, 'missing': <String>[], 'problems': <Object>[],
      'model': 'm', 'calls': 1,
    });
    expect(c.kind, 'task');
    expect(c.durationMin, 360);
    expect(c.deadline, '2026-10-30');
  });

  test('event draft takes the parsed day and times', () {
    final d = CardDraft.fromParsed(sampleCard());
    expect(d.date, const Wall(2026, 10, 30));
    expect(d.from, 18 * 60);
    expect(d.to, 20 * 60);
    expect(d.eventMinutes, 120);
    expect(d.missing(), isEmpty);
    expect(d.problems(now), isEmpty);
    expect(d.isComplete, isTrue);
  });

  test('duration gives the end when only the start is known', () {
    final d = CardDraft.fromParsed(sampleCard(timeTo: null, durationMin: 90));
    expect(d.to, 18 * 60 + 90);
  });

  test('missing fields are named, not guessed', () {
    final d = CardDraft.fromParsed(sampleCard(date: null, timeFrom: null, timeTo: null));
    expect(d.missing(), ['date', 'time_from', 'time_to']);
    expect(missingText(d.missing()), 'дата, время начала, время конца');
    expect(d.isComplete, isFalse);
  });

  test('event problems: end before start and past time', () {
    final d = CardDraft.fromParsed(sampleCard())..to = 17 * 60;
    expect(d.problems(now), ['Конец события не позже начала.']);
    expect(d.isComplete, isFalse);

    final past = CardDraft.fromParsed(sampleCard(date: '2026-10-28', timeFrom: '09:00', timeTo: '10:00'));
    expect(past.problems(now), ['Это время уже прошло.']);
  });

  test('task problems: deadline passed and unreachable', () {
    final task = sampleCard(kind: 'task', date: null, timeFrom: null, timeTo: null, durationMin: 60, deadline: '2026-10-27');
    expect(CardDraft.fromParsed(task).problems(now), ['Срок уже прошёл.']);

    final tight = sampleCard(kind: 'task', date: null, timeFrom: null, timeTo: null, durationMin: 13 * 60, deadline: '2026-10-28');
    expect(CardDraft.fromParsed(tight).problems(now), ['До срока осталось меньше времени, чем нужно на задачу.']);

    final ok = sampleCard(kind: 'task', date: null, timeFrom: null, timeTo: null, durationMin: 360, deadline: '2026-10-30');
    expect(CardDraft.fromParsed(ok).problems(now), isEmpty);
  });

  test('texts', () {
    expect(durationText(360), '6 ч');
    expect(durationText(90), '1 ч 30 мин');
    expect(durationText(45), '45 мин');
    expect(minutesHm(65), '01:05');
    expect(recurrenceText('FREQ=WEEKLY;BYDAY=FR'), 'по пятницам');
    expect(recurrenceText('FREQ=WEEKLY;BYDAY=MO,WE'), 'по понедельникам, средам');
    expect(recurrenceText('FREQ=DAILY'), 'каждый день');
    expect(recurrenceText('FREQ=YEARLY'), 'FREQ=YEARLY');
  });
}
