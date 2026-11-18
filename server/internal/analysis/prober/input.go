package prober

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// probeInput предоставляет ffprobe произвольное чтение из io.ReaderAt через
// loopback HTTP. Внешний API приложения в этом не участвует.
type probeInput struct {
	url       string
	server    *http.Server
	done      chan error
	reader    *probeReader
	requested atomic.Bool
}

// newProbeInput создаёт probeInput, который предоставляет ffprobe доступ к src через HTTP.
func newProbeInput(ctx context.Context, src io.ReaderAt, size int64) (*probeInput, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	reader := &probeReader{ctx: ctx, source: src}
	path := "/" + rand.Text() + "/audio"
	input := &probeInput{
		url:    "http://" + listener.Addr().String() + path,
		done:   make(chan error, 1),
		reader: reader,
	}
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
		input.requested.Store(true)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")

		// У каждого HTTP-запроса собственная позиция чтения.
		// ServeContent обрабатывает HEAD и Range, включая 206 и 416.
		http.ServeContent(w, r, "audio", time.Time{}, io.NewSectionReader(reader, 0, size))
	})
	input.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	go func() {
		input.done <- input.server.Serve(listener)
	}()
	return input, nil
}

// probeReader сохраняет первую ошибку источника, которую HTTP не может вернуть вызывающему коду.
type probeReader struct {
	ctx    context.Context
	source io.ReaderAt
	mu     sync.Mutex
	err    error
}

func (r *probeReader) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.ReadAt(p, offset)
	if err != nil && (!errors.Is(err, io.EOF) || n < len(p)) {
		r.mu.Lock()
		if r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *probeReader) firstError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
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
