package domain_test

import (
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// TestUnixTimeMillisRoundTrip проверяет сохранение Unix-миллисекунд при преобразовании в
// SQL-значение и обратно, включая отрицательное время.
// Для обязательного значения SQL NULL должен приводить к ошибке.
func TestUnixTimeMillisRoundTrip(t *testing.T) {
	for _, millis := range []int64{0, -1, 1700000000123} {
		original := domain.NewUnixTimeMillis(millis)
		value, err := original.Value()
		if err != nil || value != millis {
			t.Fatalf("Value(%d)=%v, %v", millis, value, err)
		}
		var scanned domain.UnixTimeMillis
		if err := scanned.Scan(value); err != nil || scanned.UnixMilli() != millis {
			t.Fatalf("Scan(%d): %v", millis, err)
		}
	}
	var nullable domain.UnixTimeMillis
	if err := nullable.Scan(nil); err == nil {
		t.Fatal("nonnullable timestamp accepted SQL NULL")
	}
	if got := domain.UnixTimeMillis(time.UnixMilli(1700000000123)).UnixMilli(); got != 1700000000123 {
		t.Fatalf("lost milliseconds: %d", got)
	}
}

// TestNormalizeIdentityRules фиксирует правила ключей идентичности: артикли сохраняются, а
// варианты AC/DC с разными разделителями или шириной символов дают один ключ.
func TestNormalizeIdentityRules(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"A", "a"}, {"The The", "the_the"}, {"The Beatles", "the_beatles"},
		{"AC/DC", "ac_dc"}, {" AC  DC ", "ac_dc"}, {"ＡＣ／ＤＣ", "ac_dc"},
		{"Кино", "кино"}, {"Café", "café"},
	} {
		if got := domain.Normalize(tc.input); got != tc.want {
			t.Errorf("Normalize(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
	if domain.Normalize("A") == domain.Normalize("The The") {
		t.Fatal("different artist identities merged")
	}
}
