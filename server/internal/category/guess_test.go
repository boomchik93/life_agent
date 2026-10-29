package category

import "testing"

func TestGuess(t *testing.T) {
	tests := []struct{ in, want string }{
		// sport
		{"Тренировка", "sport"},
		{"Зал", "sport"},
		{"Футбол с друзьями", "sport"},
		{"Йога", "sport"},
		{"Зарядка 20 минут", "sport"},
		{"Бассейн", "sport"},
		{"Пробежка в парке", "sport"},
		{"Физическая культура (практика)", "sport"},
		{"Физра", "sport"},
		{"Gym", "sport"},
		// study
		{"Матан", "study"},
		{"Лекция по физике", "study"},
		{"Лабораторная работа №3", "study"},
		{"Контрольная работа", "study"},
		{"Пара по информатике", "study"},
		{"Курсовая", "study"},
		{"Сдать лабу", "study"},
		{"Защита диплома", "study"},
		{"Английский", "study"},
		{"Семинар по философии", "study"},
		{"Консультация по диплому", "study"},
		// work
		{"Работа", "work"},
		{"Созвон по проекту", "work"},
		{"Планёрка", "work"},
		{"Собеседование", "work"},
		{"Смена в кафе", "work"},
		{"Daily sync", "work"},
		// chores
		{"Стоматолог", "chores"},
		{"К врачу", "chores"},
		{"Сдать анализы", "chores"},
		{"Магазин", "chores"},
		{"Уборка", "chores"},
		{"Забрать посылку", "chores"},
		{"Стрижка", "chores"},
		// hobby
		{"КВН", "hobby"},
		{"Гитара", "hobby"},
		{"Хор", "hobby"},
		{"Репетиция", "hobby"},
		{"Танцы", "hobby"},
		// rest
		{"Кино", "rest"},
		{"Вечеринка", "rest"},
		{"Обед с мамой", "rest"},
		{"День рождения у Димы", "rest"},
		{"Ужин", "rest"},
		{"Прогулка", "rest"},
		{"Встреча с друзьями", "rest"},
		{"Сериал", "rest"},
		{"Бар с другом", "rest"},
	}
	for _, tt := range tests {
		got, ok := Guess(tt.in)
		if !ok || got != tt.want {
			t.Errorf("Guess(%q) = %q, %v; want %q", tt.in, got, ok, tt.want)
		}
	}
}

// Words that only look like a category must not decide it.
func TestGuessNoFalseMatch(t *testing.T) {
	for _, in := range []string{
		"Встреча", "Йогурт купить", "Другое", "Хорошо подумать", "Почти готово", "Делать проект",
		"", "10:00", "Параграф", "Звонок",
	} {
		if got, ok := Guess(in); ok {
			t.Errorf("Guess(%q) = %q, want no guess", in, got)
		}
	}
}
