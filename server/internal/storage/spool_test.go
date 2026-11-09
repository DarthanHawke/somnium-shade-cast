package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/metadata"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/tags"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// newSpool создаёт буфер загрузки с заданным лимитом и завершает тест при ошибке настройки.
func newSpool(t *testing.T, limit int64) *storage.Spool {
	t.Helper()
	s, err := storage.NewSpool(limit)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestMemorySpoolContentAndReaders проверяет размер, SHA-256, независимость курсоров и
// параллельное чтение через ReadAt.
// После приёма данных каталог временных файлов должен остаться пустым.
func TestMemorySpoolContentAndReaders(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(key, dir)
	}
	data := bytes.Repeat([]byte("audio-payload"), 10000)
	b, err := newSpool(t, int64(len(data))).Receive(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if b.Size() != int64(len(data)) || b.SHA256() != hex.EncodeToString(sum[:]) {
		t.Fatal("incorrect size or hash")
	}
	first, second := b.Reader(), b.Reader()
	if _, err := first.Seek(123, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(second)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("readers share a cursor or contents differ")
	}
	var wg sync.WaitGroup
	for off := 0; off < 32; off++ {
		wg.Add(1)
		go func(off int) {
			defer wg.Done()
			buf := make([]byte, 100)
			if _, err := b.ReadAt(buf, int64(off)); err != nil || !bytes.Equal(buf, data[off:off+100]) {
				t.Error("concurrent ReadAt mismatch")
			}
		}(off)
	}
	wg.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("memory spool created temporary files")
	}
}

// TestMemorySpoolLimits проверяет отказ для недопустимого лимита и пустого файла.
// При слишком большой загрузке буфер должен прочитать не больше лимита плюс один байт.
func TestMemorySpoolLimits(t *testing.T) {
	for _, limit := range []int64{0, -1, 1<<63 - 1} {
		if _, err := storage.NewSpool(limit); err == nil {
			t.Fatalf("accepted invalid limit %d", limit)
		}
	}
	s := newSpool(t, 1000)
	if _, err := s.Receive(context.Background(), bytes.NewReader(nil)); err == nil {
		t.Fatal("accepted empty upload")
	}
	src := bytes.NewReader(make([]byte, 5000))
	if _, err := s.Receive(context.Background(), src); !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
	if src.Len() != 5000-1001 {
		t.Fatal("read beyond limit+1")
	}
}

// brokenReader имитирует источник, который сразу возвращает заданную ошибку чтения.
type brokenReader struct{ err error }

// Read возвращает заданную ошибку без данных.
func (r brokenReader) Read([]byte) (int, error) { return 0, r.err }

// cancelReader имитирует отмену контекста во время чтения.
type cancelReader struct{ cancel context.CancelFunc }

// Read отменяет контекст и возвращает один байт, чтобы проверить отмену
// после успешного чтения.
func (r cancelReader) Read(p []byte) (int, error) {
	r.cancel()
	p[0] = 'x'
	return 1, nil
}

// TestMemorySpoolFailures проверяет сохранение причины ошибки чтения
// и обработку отмены до и во время чтения.
func TestMemorySpoolFailures(t *testing.T) {
	s := newSpool(t, 1000)
	sentinel := errors.New("source disconnected")
	if _, err := s.Receive(
		context.Background(),
		brokenReader{sentinel}); !errors.Is(err, sentinel) ||
		!errors.Is(err, domain.ErrIncomplete) {
		t.Fatalf("lost read error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := s.Receive(ctx, cancelReader{cancel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if _, err := s.Receive(ctx, brokenReader{sentinel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled request read source: %v", err)
	}
}

// TestMemorySpoolToEncryptedBlob проверяет, что из буфера в памяти можно записать
// зашифрованный блоб и прочитать исходные байты запрошенного диапазона.
func TestMemorySpoolToEncryptedBlob(t *testing.T) {
	data := bytes.Repeat([]byte("memory-only audio bytes"), 20000)
	buffer, err := newSpool(t, int64(len(data))).Receive(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := storage.NewKeyManager("test", bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewBlobStore(t.TempDir(), keys)
	if err != nil {
		t.Fatal(err)
	}
	wr, err := store.Write(buffer.Reader())
	if err != nil {
		t.Fatal(err)
	}
	blob, err := domain.NewBlob(buffer.SHA256(), buffer.Size(), wr.SizeEncrypted,
		"audio/mpeg", domain.BlobAudio, wr.WrappedKey, wr.MasterKeyID, wr.ChunkSize, wr.StoragePath)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := store.OpenRange(blob, 12345, 234567)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, data[12345:12345+234567]) {
		t.Fatal("memory upload did not survive encrypted range round trip")
	}
}

// TestMemoryMetadataPipeline проверяет извлечение тегов
// и длительности MP3 из буфера в памяти.
// После анализа исходные байты должны совпасть, а каталог временных файлов остаться пустым.
func TestMemoryMetadataPipeline(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(key, dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, ffmpeg,
		"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.3",
		"-metadata", "title=Memory Title", "-metadata", "artist=Memory Artist",
		"-metadata", "album=Memory Album", "-c:a", "libmp3lame", "-f", "mp3", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := newSpool(t, int64(len(data))).Receive(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	service := metadata.New(nil, nil, prober.NewFprobe(ffprobe),
		tags.NewParser(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	meta, err := service.Extract(ctx, buffer, buffer.Size(), "original.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Memory Title" ||
		meta.ArtistName != "Memory Artist" ||
		meta.AlbumTitle != "Memory Album" ||
		meta.DurationMS == nil ||
		*meta.DurationMS <= 0 {
		t.Fatalf("incorrect extracted metadata: %+v", meta)
	}
	got, err := io.ReadAll(buffer.Reader())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("analysis changed the original bytes")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("metadata pipeline created temporary files")
	}
}
