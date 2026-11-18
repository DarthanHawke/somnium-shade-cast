// Package test проверяет работу сервера с HTTP-маршрутами, SQLite
// и зашифрованным хранилищем.
package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/metadata"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/tags"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/streaming"
)

// rig собирает интеграционное окружение с временной SQLite, хранилищем блобов и локальным
// HTTP-сервером.
type rig struct {
	t       *testing.T
	root    string
	db      *sqlite.Database
	users   *sqlite.UserRepository
	tracks  *sqlite.TrackRepository
	artists *sqlite.ArtistRepository
	albums  *sqlite.AlbumRepository
	blobs   *sqlite.BlobRepository
	search  *sqlite.SearchRepository
	library *sqlite.LibraryRepository
	store   *storage.BlobStore
	meta    *metadata.Service
	upload  *ingest.Service
	http    *httptest.Server
	log     *slog.Logger
}

// newRig открывает изолированное окружение и регистрирует закрытие HTTP-сервера и БД по
// завершении теста.
func newRig(t *testing.T) *rig {
	t.Helper()
	h := &rig{t: t, root: t.TempDir(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(func() {
		if h.http != nil {
			h.http.Close()
		}
		if h.db != nil {
			h.db.Close()
		}
	})
	h.open()
	return h
}

// open открывает БД, хранилище и HTTP-сервер на текущем корне окружения.
// После закрытия прежних ресурсов повторный вызов поднимает сервер на тех же данных.
func (h *rig) open() {
	h.t.Helper()
	var err error
	h.db, err = sqlite.Open(filepath.Join(h.root, "library.sqlite"))
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.db.Migrate(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	h.users = sqlite.NewUserRepository(h.db)
	h.tracks = sqlite.NewTrackRepository(h.db)
	h.artists = sqlite.NewArtistRepository(h.db)
	h.albums = sqlite.NewAlbumRepository(h.db)
	h.blobs = sqlite.NewBlobRepository(h.db)
	h.search = sqlite.NewSearchRepository(h.db)
	h.library = sqlite.NewLibraryRepository(h.db)
	keys, err := storage.NewKeyManager("test", bytes.Repeat([]byte{42}, 32))
	if err != nil {
		h.t.Fatal(err)
	}
	h.store, err = storage.NewBlobStore(filepath.Join(h.root, "blobs"), keys)
	if err != nil {
		h.t.Fatal(err)
	}
	h.meta = metadata.New(h.artists, h.albums, prober.NewFFprobe(""), tags.NewParser(), h.log)
	h.upload = h.service(nil, nil, nil)
	h.http = httptest.NewServer(api.NewHandlers(
		api.NewIngestHandler(h.upload, h.log), api.NewLibraryHandler(sqlite.NewLibraryRepository(h.db), h.log),
		api.NewStreamHandler(streaming.New(h.tracks, h.library, h.blobs, h.store), h.log),
		api.NewUserHandler(h.users, h.log),
	).Routes(middleware.DevAuth(h.users, h.log), middleware.Limit(2, 8), true))
	h.http.Client().Timeout = 15 * time.Second
}

// service собирает ingest-сервис с возможностью подменить анализ метаданных, репозиторий
// блобов и поиск.
// Для зависимостей, переданных как nil, берёт экземпляры окружения.
func (h *rig) service(
	meta ingest.Metadata,
	blobs ingest.BlobRepository,
	search ingest.SearchRepository,
) *ingest.Service {
	if meta == nil {
		meta = h.meta
	}
	if blobs == nil {
		blobs = h.blobs
	}
	if search == nil {
		search = h.search
	}
	spool, err := storage.NewSpool(2 << 20)
	if err != nil {
		h.t.Fatal(err)
	}
	return ingest.New(ingest.Dependencies{
		Store:    h.store,
		Spool:    spool,
		Tx:       sqlite.NewTxRepository(h.db),
		Blobs:    blobs,
		Album:    h.albums,
		Tracks:   h.tracks,
		Search:   search,
		Metadata: meta,
		Uploads:  sqlite.NewUploadRepository(h.db),
		Queue:    jobs.New(sqlite.NewJobRepository(h.db)),
	})
}

// request отправляет запрос к тестовому серверу и при заданном owner добавляет X-User.
// Тело ответа закрывает вызывающий код.
func (h *rig) request(method,
	path string,
	owner domain.PublicID,
	body io.Reader,
	headers map[string]string,
) *http.Response {
	h.t.Helper()
	r, err := http.NewRequest(method, h.http.URL+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if owner != "" {
		r.Header.Set("X-User", string(owner))
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	result, err := h.http.Client().Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}

// responseBody считывает и закрывает тело ответа, сверяет статус и возвращает прочитанные
// байты.
func responseBody(t *testing.T, result *http.Response, status int) []byte {
	t.Helper()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != status {
		t.Fatalf("status=%d, want=%d, body=%s", result.StatusCode, status, body)
	}
	return body
}

// decode декодирует JSON в указанный тип и завершает тест при ошибке.
func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("JSON %q: %v", body, err)
	}
	return value
}

// user создаёт пользователя через dev-маршрут и возвращает его запись из БД.
func (h *rig) user(name string) *domain.User {
	h.t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	result := h.request("POST", "/dev/users", "", bytes.NewReader(body),
		map[string]string{"Content-Type": "application/json"})
	user := decode[dto.UserResponse](h.t, responseBody(h.t, result, 201))
	stored, err := h.users.GetByPublicID(context.Background(), domain.PublicID(user.PublicID))
	if err != nil {
		h.t.Fatal(err)
	}
	return stored
}

// multipart собирает запрос загрузки с частями meta и file в нужном порядке и отправляет
// его в ingest.
func (h *rig) multipart(owner domain.PublicID, meta string, data []byte, name string) *http.Response {
	h.t.Helper()
	return h.multipartWithKey(owner, string(domain.NewPublicID()), meta, data, name)
}

// multipartWithKey позволяет повторить отправку с тем же ключом.
func (h *rig) multipartWithKey(owner domain.PublicID, key, meta string, data []byte, name string) *http.Response {
	h.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormField("meta")
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := io.WriteString(part, meta); err != nil {
		h.t.Fatal(err)
	}
	part, err = w.CreateFormFile("file", name)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		h.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		h.t.Fatal(err)
	}
	headers := map[string]string{"Content-Type": w.FormDataContentType()}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	return h.request(http.MethodPost, uploadsPath, owner, &body, headers)
}

// count возвращает число записей в таблице тестовой БД по имени из самого теста.
func (h *rig) count(table string) int {
	h.t.Helper()
	var n int
	if err := h.db.Get(&n, "SELECT COUNT(*) FROM "+table); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// audio генерирует синусоиду нужного формата через stdout ffmpeg.
// Пропускает тест, если недоступен ffmpeg или ffprobe.
func audio(t *testing.T, format, codec string, extra ...string) []byte {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required for real-audio checks")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe required for real-audio checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	args := []string{
		"-v", "error", "-f", "lavfi", "-i",
		"sine=frequency=440:sample_rate=48000:duration=10", "-c:a", codec}
	args = append(args, extra...)
	args = append(args, "-f", format, "pipe:1")
	data, err := exec.CommandContext(ctx, ffmpeg, args...).Output()
	if err != nil {
		t.Fatalf("generate %s: %v", format, err)
	}
	return data
}

// TestHTTPRealUploadLibraryPlaybackAndRestart проверяет загрузку настоящего MP3, единый DTO
// библиотеки, HTTP-воспроизведение и скачивание.
// После перезапуска проверяет повторную выдачу аудио. В том же сценарии проверяет
// дубликаты, изоляцию списка по владельцу и очистку блоба после удаления последней ссылки.
func TestHTTPRealUploadLibraryPlaybackAndRestart(t *testing.T) {
	data := audio(
		t, "mp3", "libmp3lame", "-b:a", "128k", "-metadata", "title=Memory Song",
		"-metadata", "artist=AC/DC", "-metadata", "album=Test Album")
	if len(data) <= domain.DefaultChunkSize+200 {
		t.Fatal("fixture must cross an encryption chunk")
	}
	h := newRig(t)
	owner := h.user("owner")
	if got := strings.TrimSpace(string(responseBody(
		t,
		h.request("GET", "/lib/tracks", owner.PublicID, nil, nil), 200))); got != "[]" {
		t.Fatalf("empty library=%s", got)
	}
	responseBody(t, h.request("GET", "/lib/tracks", "", nil, nil), 401)
	uploaded := h.readyTrackHTTP(owner.PublicID, `{"fingerprint":"client-fp","fp_duration":10}`, data, "song.mp3")
	if uploaded.Title != "Memory Song" || uploaded.Artist.Name != "AC/DC" || uploaded.Album == nil ||
		uploaded.Album.Title != "Test Album" || uploaded.DurationMS == nil || *uploaded.DurationMS < 9000 ||
		uploaded.Channels == nil || *uploaded.Channels != 1 {
		t.Fatalf("incorrect upload response: %+v", uploaded)
	}
	pid := domain.PublicID(uploaded.PublicID)
	stored, err := h.tracks.GetByPublicID(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Fingerprint != "client-fp" || stored.FPDuration != 10 {
		t.Fatal("multipart metadata lost")
	}
	var sqlDuration int64
	if err := h.db.Get(&sqlDuration, "SELECT duration_ms FROM tracks WHERE id=?", stored.ID); err != nil ||
		sqlDuration != *uploaded.DurationMS {
		t.Fatalf("SQL duration units: %d, err=%v", sqlDuration, err)
	}
	listed := decode[[]dto.TrackResponse](
		t,
		responseBody(t, h.request("GET", "/lib/tracks?limit=1", owner.PublicID, nil, nil), 200))
	if len(listed) != 1 || !reflect.DeepEqual(listed[0], uploaded) {
		t.Fatal("track detail and library list DTO mismatch")
	}
	path := "/stream/" + uploaded.PublicID
	result := h.request("GET", path, owner.PublicID, nil, nil)
	etag := result.Header.Get("ETag")
	if result.Header.Get("Content-Type") != "audio/mpeg" || result.ContentLength != int64(len(data)) {
		t.Fatal("incorrect full stream headers")
	}
	if got := responseBody(t, result, 200); !bytes.Equal(got, data) {
		t.Fatal("stream differs from original")
	}
	for _, tc := range []struct {
		header     string
		start, end int
	}{
		{"bytes=0-99", 0, 100},
		{fmt.Sprintf("bytes=%d-%d",
			domain.DefaultChunkSize-10,
			domain.DefaultChunkSize+99),
			domain.DefaultChunkSize - 10,
			domain.DefaultChunkSize + 100,
		},
		{"bytes=-31", len(data) - 31, len(data)},
		{fmt.Sprintf("bytes=%d-", len(data)-50), len(data) - 50, len(data)},
	} {
		result := h.request("GET", path, owner.PublicID, nil, map[string]string{"Range": tc.header})
		if got := responseBody(t, result, 206); !bytes.Equal(got, data[tc.start:tc.end]) {
			t.Fatalf("wrong range %s", tc.header)
		}
		want := fmt.Sprintf("bytes %d-%d/%d", tc.start, tc.end-1, len(data))
		if result.Header.Get("Content-Range") != want {
			t.Fatalf("Content-Range=%q", result.Header.Get("Content-Range"))
		}
	}
	responseBody(t, h.request("GET", path, owner.PublicID, nil,
		map[string]string{"Range": "bytes=99999999-"}), 416)
	result = h.request("HEAD", path, owner.PublicID, nil, nil)
	if result.ContentLength != int64(len(data)) || len(responseBody(t, result, 200)) != 0 {
		t.Fatal("incorrect HEAD")
	}
	responseBody(t, h.request("GET", path, owner.PublicID, nil, map[string]string{"If-None-Match": etag}), 304)
	result = h.request("GET", "/tracks/"+uploaded.PublicID+"/download", owner.PublicID, nil, nil)
	if !strings.HasPrefix(result.Header.Get("Content-Disposition"), "attachment;") ||
		!bytes.Equal(responseBody(t, result, 200), data) {
		t.Fatal("download differs")
	}
	requireUploadFailure(t, h.finishUploadHTTP(owner.PublicID, `{}`, data, "song.mp3"), "duplicate_audio")
	duplicate := h.readyTrackHTTP(owner.PublicID, `{"force_dup":true}`, data, "song.mp3")
	dupTrack, err := h.tracks.GetByPublicID(context.Background(), domain.PublicID(duplicate.PublicID))
	if err != nil {
		t.Fatal(err)
	}
	if !dupTrack.IsIntentionalDup || h.count("blobs") != 1 {
		t.Fatal("intentional duplicate did not share blob")
	}
	other := h.user("other")
	if got := string(responseBody(
		t,
		h.request("GET", "/lib/tracks", other.PublicID, nil, nil),
		200)); strings.TrimSpace(got) != "[]" {
		t.Fatal("library list crosses owner boundary")
	}
	h.http.Close()
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	h.open()
	if got := responseBody(t, h.request("GET", path, owner.PublicID, nil, nil), 200); !bytes.Equal(got, data) {
		t.Fatal("restart lost playable content")
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("audio decode failed: %v: %s", err, out)
	}
	responseBody(t, h.request(
		http.MethodDelete,
		"/lib/tracks/"+string(pid),
		owner.PublicID,
		nil,
		nil,
	), http.StatusNoContent)
	if h.count("blobs") != 1 {
		t.Fatal("deleting one reference removed shared blob")
	}
	responseBody(t, h.request("GET", "/stream/"+duplicate.PublicID, owner.PublicID, nil, nil), 200)
	responseBody(t, h.request(
		http.MethodDelete,
		"/lib/tracks/"+duplicate.PublicID,
		owner.PublicID,
		nil,
		nil,
	), http.StatusNoContent)

	if h.count("blobs") != 1 || h.count("tracks") != 0 {
		t.Fatal("expected an unreferenced blob and no tracks")
	}
	var refCount int
	if err := h.db.Get(&refCount, "SELECT ref_count FROM blobs"); err != nil || refCount != 0 {
		t.Fatalf("ref_count=%d, want 0: %v", refCount, err)
	}

	responseBody(t, h.request("GET", path, owner.PublicID, nil, nil), 404)
	responseBody(t, h.request("GET", path, owner.PublicID, nil, nil), 404)
}

// fixedMetadata подменяет извлечение тегов предсказуемыми данными и использует обычный
// сервис для остальных операций.
type fixedMetadata struct{ *metadata.Service }

// Extract возвращает заданные метаданные независимо от содержимого входного файла.
func (m fixedMetadata) Extract(context.Context, io.ReaderAt, int64, string) (*domain.ExtractedMetadata, error) {
	durationMS := int64(123456)
	return &domain.ExtractedMetadata{
		Title:      "File Title",
		ArtistName: "File Artist",
		AlbumTitle: "File Album",
		DiscNo:     1,
		DurationMS: &durationMS,
		Codec:      "mp3",
		MIMEType:   "audio/mpeg",
	}, nil
}

// fixedService создаёт ingest-сервис с фиксированными метаданными и необязательной подменой
// репозиториев блобов и поиска.
func (h *rig) fixedService(blobs ingest.BlobRepository, search ingest.SearchRepository) *ingest.Service {
	return h.service(fixedMetadata{h.meta}, blobs, search)
}

// barrierBlobs синхронизирует первые два поиска по SHA-256, чтобы обе загрузки увидели
// состояние БД до регистрации блоба.
type barrierBlobs struct {
	*sqlite.BlobRepository
	arrived atomic.Int32
	ready   chan struct{}
}

// GetBySHA256 выполняет поиск и задерживает первые два результата, пока оба вызова не
// дойдут до барьера или контекст не отменится.
func (b *barrierBlobs) GetBySHA256(ctx context.Context, sha string) (*domain.Blob, error) {
	value, err := b.BlobRepository.GetBySHA256(ctx, sha)
	n := b.arrived.Add(1)
	if n <= 2 {
		if n == 2 {
			close(b.ready)
		}
		select {
		case <-b.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return value, err
}

// TestConcurrentUploadsShareBlobWithoutCorruption проверяет дедупликацию параллельных
// загрузок, читаемость общего блоба и точный счётчик ссылок.
// После завершения загрузок GC должен оставить только зарегистрированный файл.
func TestConcurrentUploadsShareBlobWithoutCorruption(t *testing.T) {
	for _, tc := range []struct {
		name             string
		sameOwner, force bool
		successes        int
	}{
		{"different-owners", false, false, 2},
		{"same-owner", true, false, 1},
		{"intentional-duplicates", true, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRig(t)
			a, b := h.user("a"), h.user("b")
			if tc.sameOwner {
				b = a
			}
			svc := h.fixedService(&barrierBlobs{BlobRepository: h.blobs, ready: make(chan struct{})}, nil)
			data := bytes.Repeat([]byte("same plaintext"), 20000)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type outcome struct {
				item *domain.TrackUpload
				err  error
			}
			done := make(chan outcome, 2)
			for _, owner := range []*domain.User{a, b} {
				go func(owner *domain.User) {
					item, err := svc.Accept(ctx, ingest.UploadInput{
						OwnerID:          owner.ID,
						ForceDup:         tc.force,
						OriginalFilename: "same.mp3",
					}, string(domain.NewPublicID()), bytes.NewReader(data))
					done <- outcome{item, err}
				}(owner)
			}
			accepted := make([]*domain.TrackUpload, 0, 2)
			for range 2 {
				out := <-done
				if out.err != nil {
					t.Fatal(out.err)
				}
				if out.item == nil || out.item.JobID == nil {
					t.Fatal("accepted upload has no job")
				}
				accepted = append(accepted, out.item)
			}

			stop := h.startUploadWorker(svc.Process)
			defer stop()
			successes := 0
			for _, item := range accepted {
				final := h.waitUploadItem(ctx, item)
				h.waitJobStatus(ctx, *item.JobID, "done")
				if final.Status != domain.UploadReady {
					requireUploadFailure(t, final, "duplicate_audio")
					continue
				}
				successes++
				if final.TrackPublicID == nil {
					t.Fatalf("completed upload has no track: %+v", final)
				}
				track, err := h.tracks.GetByPublicID(ctx, *final.TrackPublicID)
				if err != nil || track.BlobID == nil {
					t.Fatalf("track = %+v, err=%v", track, err)
				}
				blob, err := h.blobs.GetByID(ctx, *track.BlobID)
				if err != nil {
					t.Fatal(err)
				}
				r, err := h.store.Open(blob)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(r)
				r.Close()
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("concurrent write corrupted blob: %v", err)
				}
			}
			stop()
			if successes != tc.successes || h.count("blobs") != 1 || h.count("tracks") != successes {
				t.Fatalf("unexpected persisted counts, successes=%d", successes)
			}
			var refCount int
			if err := h.db.Get(&refCount, "SELECT ref_count FROM blobs"); err != nil || refCount != successes {
				t.Fatalf("ref_count=%d err=%v", refCount, err)
			}
			// Проигравший файл разрешено оставить для GC. Проверяем, что GC
			// после завершения загрузок удаляет только мусор и сохраняет победителя.
			if _, err := h.store.GC(ctx, h.blobs, 0); err != nil {
				t.Fatal(err)
			}
			var files int
			if err := filepath.WalkDir(filepath.Join(h.root, "blobs"),
				func(_ string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() {
						files++
					}
					return nil
				}); err != nil || files != 1 {
				t.Fatalf("GC left %d files, err=%v", files, err)
			}
		})
	}
}

// failingSearch подменяет запись в поисковый индекс заданной ошибкой для проверки отката
// загрузки.
type failingSearch struct {
	*sqlite.SearchRepository
	failure error
}

// IndexTrack записывает индекс и имитирует сбой до завершения транзакции.
func (f failingSearch) IndexTrack(ctx context.Context, id domain.ID) error {
	if err := f.SearchRepository.IndexTrack(ctx, id); err != nil {
		return err
	}
	return f.failure
}

// TestUploadRollbackPreservesFileForRetry проверяет откат трека и FTS при
// временной ошибке, удержание принятого файла и повтор той же загрузки.
func TestUploadRollbackPreservesFileForRetry(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	ctx := t.Context()
	sentinel := fmt.Errorf("FTS unavailable: %w", domain.ErrNotFound)
	svc := h.fixedService(nil, failingSearch{h.search, sentinel})
	item, err := svc.Accept(ctx, ingest.UploadInput{OwnerID: owner.ID}, "retry-fts", strings.NewReader("audio"))
	if err != nil {
		t.Fatal(err)
	}
	h.waitFailedAttempt(ctx, svc, item, sentinel)

	if h.count("tracks") != 0 || h.count("blobs") != 1 || h.count("tracks_fts") != 0 {
		t.Fatal("failed attempt saved a track or lost its staged file")
	}
	if h.count("artists") != 0 || h.count("albums") != 0 {
		t.Fatal("rollback left file-tag artist or album")
	}
	stored, err := h.upload.Get(ctx, owner.ID, item.PublicID)
	if err != nil || stored.Status != domain.UploadQueued || stored.BlobID == nil {
		t.Fatalf("upload unavailable for retry: %+v, %v", stored, err)
	}
	blob, err := h.blobs.GetByID(ctx, *stored.BlobID)
	if err != nil || blob.RefCount != 0 {
		t.Fatalf("reference count survived rollback: %+v, %v", blob, err)
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE jobs SET run_after = 0 WHERE id = ?", *item.JobID); err != nil {
		t.Fatal(err)
	}
	stop := h.startUploadWorker(h.fixedService(nil, nil).Process)
	defer stop()
	final := h.waitUploadItem(ctx, item)
	if final.Status != domain.UploadReady || final.TrackPublicID == nil ||
		h.count("tracks") != 1 || h.count("blobs") != 1 || h.count("tracks_fts") != 1 {
		t.Fatalf("retry did not finish original upload: %+v", final)
	}
}

// ghost создаёт трек без аудиофайла с исполнителем и, по запросу, альбомом.
// Номер диска 2 позволяет заметить подмену нумерации метаданными файла.
func (h *rig) ghost(owner *domain.User, withAlbum bool) *domain.Track {
	h.t.Helper()
	ctx := context.Background()
	artist, err := domain.NewArtist("Ghost Artist")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.artists.Insert(ctx, artist); err != nil {
		h.t.Fatal(err)
	}
	var albumID *domain.ID
	if withAlbum {
		album, err := domain.NewAlbum(artist.ID, "Ghost Album", nil, nil)
		if err != nil {
			h.t.Fatal(err)
		}
		if err := h.albums.Insert(ctx, album); err != nil {
			h.t.Fatal(err)
		}
		albumID = &album.ID
	}
	ghost, err := domain.NewTrack(domain.NewTrackParams{
		OwnerID:          owner.ID,
		ArtistID:         artist.ID,
		AlbumID:          albumID,
		Title:            "Ghost Track",
		TrackNo:          nil,
		DiscNo:           2,
		DurationMS:       nil,
		Status:           domain.TrackGhost,
		BlobID:           nil,
		Fingerprint:      "",
		FPDuration:       0,
		IsIntentionalDup: false,
		OriginalFilename: "",
		Codec:            "",
		BitrateKbps:      nil,
		SampleRate:       nil,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.tracks.Insert(ctx, ghost); err != nil {
		h.t.Fatal(err)
	}
	return ghost
}

// TestGhostMaterializationPreservesIdentity проверяет, что загрузка прикрепляет файл к
// существующему ghost и сохраняет его идентичность, нумерацию и связь с альбомом.
// Повторная материализация отклоняется, а после удаления в поисковом индексе не остаётся
// записи.
func TestGhostMaterializationPreservesIdentity(t *testing.T) {
	for _, withAlbum := range []bool{false, true} {
		t.Run(fmt.Sprintf("album=%t", withAlbum), func(t *testing.T) {
			h := newRig(t)
			owner := h.user("owner")
			ghost := h.ghost(owner, withAlbum)
			artistsBefore, albumsBefore := h.count("artists"), h.count("albums")
			svc := h.fixedService(nil, nil)
			data := []byte("ghost audio")
			track := h.uploadTrack(t.Context(), svc, ingest.UploadInput{
				OwnerID:     owner.ID,
				GhostID:     &ghost.PublicID,
				ForceDup:    true,
				Fingerprint: "ghost-fp",
				FPDuration:  12,
			}, data)
			if got := h.count("artists"); got != artistsBefore {
				t.Errorf("ghost upload created unused artist: got %d, want %d",
					got, artistsBefore)
			}
			if got := h.count("albums"); got != albumsBefore {
				t.Errorf("ghost upload created unused album: got %d, want %d",
					got, albumsBefore)
			}
			path := "/lib/tracks/" + string(ghost.PublicID)
			view := decode[dto.TrackResponse](t, responseBody(t,
				h.request(http.MethodGet, path, owner.PublicID, nil, nil), http.StatusOK))
			if track.PublicID != ghost.PublicID || track.Title != ghost.Title ||
				track.ArtistID != ghost.ArtistID || view.Artist.Name != "Ghost Artist" || track.DiscNo != 2 {
				t.Fatal("file tags replaced ghost identity")
			}
			if withAlbum && (view.Album == nil || view.Album.Title != "Ghost Album") {
				t.Fatal("ghost album lost")
			}
			if !withAlbum && view.Album != nil {
				t.Fatal("file album attached to albumless ghost")
			}
			stored, err := h.tracks.GetByPublicID(context.Background(), ghost.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != domain.TrackUploaded ||
				stored.BlobID == nil ||
				stored.Fingerprint != "ghost-fp" ||
				stored.FPDuration != 12 ||
				!stored.IsIntentionalDup {
				t.Errorf("materialization fields not persisted: %+v", stored)
			}
			if h.count("tracks") != 1 {
				t.Fatal("materialization created another track")
			}
			rejected := h.processUpload(t.Context(), svc, ingest.UploadInput{
				OwnerID: owner.ID, GhostID: &ghost.PublicID, ForceDup: true,
			}, data)
			requireUploadFailure(t, rejected, "target_unavailable")
			responseBody(t, h.request(http.MethodDelete, path, owner.PublicID, nil, nil), http.StatusNoContent)
		})
	}
}

// TestGhostRejectsDifferentOwnerAndMalformedID проверяет отказ при попытке материализовать
// чужой ghost или передать некорректный ID, без создания дополнительных записей.
func TestGhostRejectsDifferentOwnerAndMalformedID(t *testing.T) {
	h := newRig(t)
	a, b := h.user("a"), h.user("b")
	ghost := h.ghost(a, true)
	svc := h.fixedService(nil, nil)
	rejected := h.processUpload(t.Context(), svc, ingest.UploadInput{
		OwnerID: b.ID, GhostID: &ghost.PublicID,
	}, []byte("audio"))
	requireUploadFailure(t, rejected, "target_unavailable")
	bad := domain.PublicID("invalid")
	_, err := svc.Accept(t.Context(), ingest.UploadInput{OwnerID: a.ID, GhostID: &bad},
		"bad-ghost", strings.NewReader("audio"))
	validation, ok := errors.AsType[*domain.ValidationError](err)
	if !ok || validation.Field != "ghost_id" {
		t.Fatalf("malformed ghost ID validation: %v", err)
	}
	if h.count("track_uploads") != 1 || h.count("tracks") != 1 {
		t.Fatal("failed materialization changed track or queued malformed input")
	}
	blob, err := h.upload.Get(t.Context(), b.ID, rejected.PublicID)
	if err != nil || blob.BlobID != nil {
		t.Fatalf("failed materialization kept audio pinned: %+v, %v", blob, err)
	}
}

// TestUploadWithoutAlbumAndInvalidRequests проверяет загрузку трека без альбома и ошибки
// для некорректных метаданных, файла и запросов к библиотеке.
func TestUploadWithoutAlbumAndInvalidRequests(t *testing.T) {
	data := audio(t, "mp3", "libmp3lame", "-metadata", "title=Standalone", "-metadata", "artist=Artist")
	h := newRig(t)
	owner := h.user("owner")
	track := h.readyTrackHTTP(owner.PublicID, `{}`, data, "single.mp3")
	if track.Album != nil || track.CoverURL != nil {
		t.Fatal("absent album/cover must be null")
	}
	for i, meta := range []string{
		`{"unknown":true}`,
		`{"owner_id":999}`,
		`{} {}`,
		`{}` + strings.Repeat(" ", 65536),
	} {
		t.Run(fmt.Sprintf("meta-%d", i), func(t *testing.T) {
			responseBody(t, h.multipart(owner.PublicID, meta, data, "single.mp3"), 400)
		})
	}
	responseBody(t, h.multipart(owner.PublicID, `{}`, nil, "empty.mp3"), 400)
	requireUploadFailure(t, h.finishUploadHTTP(
		owner.PublicID, `{}`, []byte("not audio"), "bad.mp3"), "unsupported_audio")
	responseBody(t, h.multipartWithKey(owner.PublicID, "", `{}`, data, "single.mp3"), http.StatusBadRequest)
	responseBody(t, h.multipart(owner.PublicID, `{}`, make([]byte, (2<<20)+1), "big.mp3"), 413)
	responseBody(t, h.request(
		"POST",
		uploadsPath,
		owner.PublicID,
		strings.NewReader("bad"),
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "wrong-content-type"},
	), 400)
	responseBody(t, h.request(
		"GET",
		"/lib/tracks/not-an-id",
		owner.PublicID,
		nil,
		nil,
	), 400)
	responseBody(t, h.request(
		"GET",
		"/lib/tracks/"+string(domain.NewPublicID()),
		owner.PublicID,
		nil,
		nil,
	), 404)
}

// TestMissingAndCorruptBlobReturnCleanErrors проверяет JSON-ответы 410 для отсутствующего
// файла и 500 для повреждённого первого чанка.
// В ответ не должны попадать заголовки аудиодиапазона.
func TestMissingAndCorruptBlobReturnCleanErrors(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			h := newRig(t)
			owner := h.user("owner")
			track := h.uploadTrack(t.Context(), h.fixedService(nil, nil),
				ingest.UploadInput{OwnerID: owner.ID}, bytes.Repeat([]byte{42}, 100000))
			blob, err := h.blobs.GetByID(context.Background(), *track.BlobID)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(h.root, "blobs", blob.StoragePath)
			status := 500
			if missing {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				status = 410
			} else {
				f, err := os.OpenFile(path, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				b := []byte{0}
				_, err = f.ReadAt(b, 20)
				if err == nil {
					b[0] ^= 255
					_, err = f.WriteAt(b, 20)
				}
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			response := h.request(
				"GET",
				"/stream/"+string(track.PublicID),
				owner.PublicID,
				nil,
				map[string]string{"Range": "bytes=0-99"},
			)
			body := responseBody(t, response, status)
			if response.Header.Get("Content-Range") != "" ||
				!strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") ||
				response.ContentLength != int64(len(body)) {
				t.Fatalf("audio headers leaked into JSON: %v, body=%s", response.Header, body)
			}
		})
	}
}

// TestUploadRejectsMissingBlob проверяет, что force_dup не добавляет трек
// и не увеличивает счётчик ссылок, если файл существующего блоба исчез.
func TestUploadRejectsMissingBlob(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	svc := h.fixedService(nil, nil)
	ctx := context.Background()
	data := []byte("fixed audio content")
	input := ingest.UploadInput{OwnerID: owner.ID, OriginalFilename: "track.mp3"}

	uploaded := h.uploadTrack(ctx, svc, input, data)
	blobID := *uploaded.BlobID
	blob, err := h.blobs.GetByID(ctx, blobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Remove(blob.StoragePath); err != nil {
		t.Fatal(err)
	}

	input.ForceDup = true
	result, err := svc.Accept(ctx, input, "missing-blob", bytes.NewReader(data))
	if result != nil || !errors.Is(err, domain.ErrBlobFileMissing) {
		t.Fatalf("result=%v, err=%v, want ErrBlobFileMissing", result, err)
	}
	if h.count("tracks") != 1 || h.count("track_uploads") != 1 {
		t.Fatal("missing blob registered an additional track or upload")
	}

	blob, err = h.blobs.GetByID(ctx, blobID)
	if err != nil {
		t.Fatal(err)
	}
	if blob.RefCount != 1 {
		t.Fatalf("ref_count=%d, want 1", blob.RefCount)
	}
}

// TestContainerMimeMatchesStream проверяет MIME, который анализатор определяет по
// контейнеру для AAC в MP4 и Opus в Ogg.
func TestContainerMimeMatchesStream(t *testing.T) {
	for _, tc := range []struct {
		format, encoder, mime string
		extra                 []string
	}{
		{"mp4", "aac", "audio/mp4", []string{"-movflags", "frag_keyframe+empty_moov"}},
		{"opus", "libopus", "audio/ogg", nil},
	} {
		t.Run(tc.format, func(t *testing.T) {
			data := audio(t, tc.format, tc.encoder, tc.extra...)
			got, err := prober.NewFFprobe("").Probe(
				context.Background(),
				bytes.NewReader(data),
				int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got.MIMEType != tc.mime {
				t.Fatalf("container %s returned %s, want %s", tc.format, got.MIMEType, tc.mime)
			}
		})
	}
}

// rejectingResolveMetadata возвращает фиксированные теги, но завершает Resolve заданной
// ошибкой.
type rejectingResolveMetadata struct {
	fixedMetadata
	err error
}

// Resolve возвращает заданную ошибку без поиска или создания исполнителя и альбома.
func (m rejectingResolveMetadata) Resolve(
	context.Context,
	*domain.ExtractedMetadata,
) (*domain.Artist, *domain.Album, error) {
	return nil, nil, m.err
}

// TestUploadMetadataResolveOnlyForNewTrack проверяет, что ошибка Resolve останавливает
// создание нового трека, а материализация ghost проходит без вызова Resolve.
func TestUploadMetadataResolveOnlyForNewTrack(t *testing.T) {
	for _, ghostUpload := range []bool{false, true} {
		name := "new-track"
		if ghostUpload {
			name = "ghost"
		}
		t.Run(name, func(t *testing.T) {
			h := newRig(t)
			owner := h.user("owner")
			sentinel := errors.New("resolve must not run for ghost")
			svc := h.service(rejectingResolveMetadata{fixedMetadata{h.meta}, sentinel}, nil, nil)
			input := ingest.UploadInput{OwnerID: owner.ID}
			if ghostUpload {
				ghost := h.ghost(owner, true)
				input.GhostID = &ghost.PublicID
			}
			if ghostUpload {
				track := h.uploadTrack(t.Context(), svc, input, []byte("audio"))
				if track.PublicID != *input.GhostID {
					t.Fatal("ghost identity changed")
				}
				return
			}
			item, err := svc.Accept(t.Context(), input, "resolve-error", strings.NewReader("audio"))
			if err != nil {
				t.Fatal(err)
			}
			h.waitFailedAttempt(t.Context(), svc, item, sentinel)
			if h.count("tracks") != 0 || h.count("blobs") != 1 {
				t.Fatal("resolve failure created a track or discarded the file needed for retry")
			}
		})
	}
}
