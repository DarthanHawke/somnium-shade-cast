package prober

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// probeInput предоставляет ffprobe произвольное чтение из памяти через
// loopback HTTP. Внешний API приложения в этом не участвует.
type probeInput struct {
	url    string
	server *http.Server
	done   chan error
}

// newProbeInput создаёт probeInput, который предоставляет ffprobe доступ к src через HTTP.
func newProbeInput(ctx context.Context, src io.ReaderAt, size int64) (*probeInput, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	path := "/" + rand.Text() + "/audio"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")

		// У каждого HTTP-запроса собственная позиция чтения.
		// ServeContent обрабатывает HEAD и Range, включая 206 и 416.
		http.ServeContent(w, r, "audio", time.Time{}, io.NewSectionReader(src, 0, size))
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	input := &probeInput{
		url:    "http://" + listener.Addr().String() + path,
		server: server,
		done:   make(chan error, 1),
	}
	go func() {
		input.done <- server.Serve(listener)
	}()
	return input, nil
}

// Close останавливает HTTP-сервер и ждёт завершения Serve. Вызывается один раз.
func (p *probeInput) Close() error {
	closeErr := p.server.Close()
	serveErr := <-p.done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(closeErr, serveErr)
}
