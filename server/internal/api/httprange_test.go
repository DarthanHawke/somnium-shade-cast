package api

import "testing"

// TestParseRange проверяет разбор одиночных байтовых диапазонов, ограничение концом файла и
// отказ для некорректных заголовков.
func TestParseRange(t *testing.T) {
	for _, tc := range []struct {
		header         string
		offset, length int64
		ranged, bad    bool
	}{
		{"", 0, -1, false, false},
		{"bytes=0-0", 0, 1, true, false},
		{"bytes=90-200", 90, 10, true, false},
		{"bytes=90-", 90, -1, true, false},
		{"bytes=-10", 90, 10, true, false},
		{"bytes=-200", 0, 100, true, false},
		{"bytes=100-", 0, 0, false, true},
		{"bytes=20-10", 0, 0, false, true},
		{"bytes=-0", 0, 0, false, true},
		{"bytes=0-1,5-6", 0, 0, false, true},
		{"items=0-1", 0, 0, false, true},
		{"bytes=x-2", 0, 0, false, true},
		{"bytes=999999999999999999999-", 0, 0, false, true},
	} {
		t.Run(tc.header, func(t *testing.T) {
			got, err := parseRange(tc.header, 100)
			if tc.bad {
				if err == nil {
					t.Fatal("invalid range accepted")
				}
				return
			}
			if err != nil || got.offset != tc.offset || got.length != tc.length || got.isRange != tc.ranged {
				t.Fatalf("range=%+v err=%v", got, err)
			}
		})
	}
}

// FuzzParseRange проверяет, что любой принятый диапазон начинается внутри файла и его явная
// длина укладывается в остаток файла.
func FuzzParseRange(f *testing.F) {
	for _, seed := range []string{"", "bytes=0-9", "bytes=-1", "bytes=999-", "bytes=0-1,3-4"} {
		f.Add(seed, int64(100))
	}
	f.Fuzz(func(t *testing.T, header string, size int64) {
		if size <= 0 {
			return
		}
		r, err := parseRange(header, size)
		if err != nil {
			return
		}
		if r.offset < 0 || r.offset >= size {
			t.Fatalf("offset=%d size=%d", r.offset, size)
		}
		if r.length != -1 && (r.length <= 0 || r.length > size-r.offset) {
			t.Fatalf("length=%d offset=%d size=%d", r.length, r.offset, size)
		}
	})
}
