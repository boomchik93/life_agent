import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:life_agent/format.dart';
import 'package:life_agent/models.dart';

void main() {
  group('Wall', () {
    test('keeps the wall clock of the server zone, ignores the offset', () {
      final w = Wall.parse('2026-10-27T18:30:00+05:00');
      expect((w.hour, w.minute, w.day), (18, 30, 27));
      expect(Wall.parse('2026-10-27T00:00:00-08:00').hm, '00:00');
    });

    test('day arithmetic crosses month and year', () {
      expect(const Wall(2026, 10, 31).addDays(1), const Wall(2026, 11, 1));
      expect(const Wall(2026, 12, 31).addDays(1), const Wall(2027, 1, 1));
      expect(const Wall(2026, 3, 1).addDays(-1), const Wall(2026, 2, 28));
    });

    test('weekday: 2026-10-26 is a Monday', () {
      expect(const Wall(2026, 10, 26).weekday, 1);
      expect(const Wall(2026, 11, 1).weekday, 7);
    });

    test('rejects garbage', () {
      expect(() => Wall.parse('tomorrow'), throwsFormatException);
    });
  });

  group('CalEvent.touches', () {
    CalEvent ev(Wall s, Wall e, {bool allDay = false}) => CalEvent(
        id: 'x', source: 'google', calendar: 'c', title: 't', start: s, end: e, allDay: allDay);
    const mon = Wall(2026, 10, 26), tue = Wall(2026, 10, 27);

    test('overnight event shows on both days', () {
      final e = ev(const Wall(2026, 10, 26, 22), const Wall(2026, 10, 27, 4));
      expect(e.touches(mon), isTrue);
      expect(e.touches(tue), isTrue);
      expect(eventTime(e, mon), 'с 22:00');
      expect(eventTime(e, tue), 'до 04:00');
    });

    test('event ending at midnight stays on its day only', () {
      final e = ev(const Wall(2026, 10, 26, 20), const Wall(2026, 10, 27));
      expect(e.touches(mon), isTrue);
      expect(e.touches(tue), isFalse);
      expect(eventTime(e, mon), '20:00–00:00');
    });

    test('all-day event with exclusive end', () {
      final e = ev(mon, const Wall(2026, 10, 28), allDay: true);
      expect(e.touches(mon), isTrue);
      expect(e.touches(tue), isTrue);
      expect(e.touches(const Wall(2026, 10, 28)), isFalse);
      expect(eventTime(e, mon), 'весь день');
    });

    test('event without duration is an instant', () {
      final e = ev(const Wall(2026, 10, 26, 9), const Wall(2026, 10, 26, 9));
      expect(e.touches(mon), isTrue);
      expect(e.touches(tue), isFalse);
      expect(eventTime(e, mon), '09:00');
    });
  });

  test('WeekData parses the server response', () {
    final json = jsonDecode('''
    {"from":"2026-10-26T00:00:00+05:00","to":"2026-11-02T00:00:00+05:00",
     "events":[{"id":"a","source":"ics","calendar":"Расписание","title":"Пара",
       "start":"2026-10-26T08:00:00+05:00","end":"2026-10-26T09:30:00+05:00","all_day":false,"recurring":true}],
     "warnings":["ics: timeout"],
     "risks":{
       "sleep":{"color":"gray","reasons":["нет данных"],"metrics":{}},
       "overload":{"color":"yellow","reasons":["много"],"metrics":{"week_hours":36.5}},
       "no_rest":{"color":"green","reasons":[],"metrics":{"free_evenings":6}},
       "state":{"color":"purple","reasons":[],"metrics":{}}}}''') as Map<String, dynamic>;
    final w = WeekData.fromJson(json);
    expect(w.days.length, 7);
    expect(w.eventsOn(w.days.first).single.title, 'Пара');
    expect(w.eventsOn(w.days[1]), isEmpty);
    expect(w.risks.overload.color, RiskColor.yellow);
    expect(w.risks.overload.metrics['week_hours'], 36.5);
    expect(w.risks.noRest.metrics['free_evenings'], 6.0);
    expect(w.risks.state.color, RiskColor.gray, reason: 'unknown colour must not look healthy');
    expect(w.warnings, ['ics: timeout']);
  });

  test('weekRange', () {
    expect(weekRange(const Wall(2026, 10, 26), const Wall(2026, 11, 2)), '26 октября – 1 ноября');
  });
}
