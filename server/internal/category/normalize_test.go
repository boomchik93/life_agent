package category

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"КВН", "квн"},
		{"КВН в пятницу с 18:00 до 20:00", "квн"},
		{"  Математика, 3 пара ", "математика пара"},
		{"Курсовая до 5 ноября", "курсовая"},
		{"Зал, завтра в 19.30", "зал"},
		{"Ёлка", "елка"},
		{"в на с", ""},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
