// Package category keys the category memory: a title is reduced to its
// meaning, without dates, times, weekdays and prepositions.
package category

import (
	"regexp"
	"strings"
)

var (
	timeRe  = regexp.MustCompile(`\d{1,2}[:.]\d{2}`)
	digitRe = regexp.MustCompile(`\d+`)
	junkRe  = regexp.MustCompile(`[^\p{L}\s]+`)
)

var stop = map[string]bool{
	// prepositions and glue words
	"в": true, "во": true, "на": true, "с": true, "со": true, "до": true, "по": true, "к": true, "ко": true,
	"у": true, "и": true, "за": true, "от": true, "из": true, "для": true, "через": true, "после": true,
	// days and dates
	"сегодня": true, "завтра": true, "послезавтра": true, "вчера": true,
	"понедельник": true, "вторник": true, "среду": true, "среда": true, "четверг": true, "пятницу": true,
	"пятница": true, "субботу": true, "суббота": true, "воскресенье": true,
	"пн": true, "вт": true, "ср": true, "чт": true, "пт": true, "сб": true, "вс": true,
	"января": true, "февраля": true, "марта": true, "апреля": true, "мая": true, "июня": true,
	"июля": true, "августа": true, "сентября": true, "октября": true, "ноября": true, "декабря": true,
	"утром": true, "днём": true, "днем": true, "вечером": true, "ночью": true,
	"каждый": true, "каждую": true, "каждое": true, "неделю": true, "неделе": true, "этой": true, "следующей": true,
	"час": true, "часа": true, "часов": true, "минут": true, "мин": true, "ч": true,
}

// Normalize returns the key of a title: lower case, no digits, times or
// punctuation, no weekday, month or preposition words.
func Normalize(title string) string {
	s := strings.ReplaceAll(strings.ToLower(title), "ё", "е")
	s = timeRe.ReplaceAllString(s, " ")
	s = digitRe.ReplaceAllString(s, " ")
	s = junkRe.ReplaceAllString(s, " ")
	var words []string
	for _, w := range strings.Fields(s) {
		if !stop[w] {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}
