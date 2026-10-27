import 'models.dart';

const weekdayShort = ['пн', 'вт', 'ср', 'чт', 'пт', 'сб', 'вс'];
const weekdayLong = [
  'Понедельник', 'Вторник', 'Среда', 'Четверг', 'Пятница', 'Суббота', 'Воскресенье'
];
const _monthGen = [
  'января', 'февраля', 'марта', 'апреля', 'мая', 'июня',
  'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'
];

String dayTitle(Wall d) => '${weekdayLong[d.weekday - 1]}, ${d.day} ${_monthGen[d.month - 1]}';

String shortDate(Wall d) => '${d.day} ${_monthGen[d.month - 1]}';

/// "26 октября – 1 ноября" for the week [from, to).
String weekRange(Wall from, Wall to) => '${shortDate(from)} – ${shortDate(to.addDays(-1))}';

Wall wallNow([DateTime? now]) {
  final n = now ?? DateTime.now();
  return Wall(n.year, n.month, n.day, n.hour, n.minute);
}

/// Time label of an event on [day]: part of a multi-day event is shown with its
/// real start or end.
String eventTime(CalEvent e, Wall day) {
  if (e.allDay) return 'весь день';
  final startsToday = e.start.sameDay(day);
  final endsToday = e.end.sameDay(day) || e.end == day.dayStart.addDays(1);
  if (startsToday && endsToday) return e.end == e.start ? e.start.hm : '${e.start.hm}–${e.end.hm}';
  if (startsToday) return 'с ${e.start.hm}';
  if (e.end.sameDay(day)) return 'до ${e.end.hm}';
  return 'весь день';
}
