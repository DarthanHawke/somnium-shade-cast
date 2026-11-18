package prober

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// localClient создаёт HTTP-клиент для тестов loopback-сервера с ограниченным временем
// запроса и отключённым повторным использованием соединений.
func localClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		Timeout:   5 * time.Second,
	}
}

// TestProbeInputRangesAndLifetime проверяет GET, HEAD, корректные и некорректные диапазоны,
// неверный путь и неподдерживаемый метод локального HTTP-источника.
// Параллельные запросы диапазонов должны читать каждый свой фрагмент.
func TestProbeInputRangesAndLifetime(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 10000)
	input, err := newProbeInput(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	client := localClient()
	for _, tc := range []struct {
		method, suffix, rng string
		status              int
		want                []byte
	}{
		{"GET", "", "bytes=13-77", 206, data[13:78]},
		{"GET", "", "bytes=-31", 206, data[len(data)-31:]},
		{"GET", "", "bytes=99999-", 206, data[99999:]},
		{"HEAD", "", "", 200, nil},
		{"GET", "", "", 200, data},
		{"GET", "/wrong", "", 404, nil},
		{"POST", "", "", 405, nil},
		{"GET", "", "bytes=100000-", 416, nil},
	} {
		t.Run(fmt.Sprintf("%s/%s/%d", tc.method, tc.rng, tc.status), func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, input.url+tc.suffix, nil)
			if tc.rng != "" {
				req.Header.Set("Range", tc.rng)
			}
			result, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Body.Close()
			body, err := io.ReadAll(result.Body)
			if err != nil || result.StatusCode != tc.status {
				t.Fatalf("status=%d err=%v", result.StatusCode, err)
			}
			if tc.status < 400 && !bytes.Equal(body, tc.want) {
				t.Fatal("incorrect response bytes")
			}
			if tc.status == 206 && !strings.HasPrefix(result.Header.Get("Content-Range"), "bytes ") {
				t.Fatal("missing range header")
			}
		})
	}
	var wg sync.WaitGroup
	for off := range 20 {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", input.url, nil)
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+999))
			result, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer result.Body.Close()
			body, err := io.ReadAll(result.Body)
			if err != nil || !bytes.Equal(body, data[off:off+1000]) {
				t.Error("concurrent HTTP cursor corruption")
			}
		}(off)
	}
	wg.Wait()
}

// TestProbeInputClosesListener проверяет, что после Close локальный HTTP-источник перестаёт
// принимать соединения.
func TestProbeInputClosesListener(t *testing.T) {
	input, err := newProbeInput(context.Background(), bytes.NewReader([]byte("audio")), 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := localClient().Get(input.url); err == nil {
		result.Body.Close()
		t.Fatal("listener survived Close")
	}
}
