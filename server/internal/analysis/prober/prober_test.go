package prober_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
)

// TestFfprobeMemoryFormats проверяет кодек, длительность и частоту дискретизации для
// нескольких аудиоформатов, поданных из памяти.
// После анализа каталог, заданный для временных файлов, должен остаться пустым.
func TestFfprobeMemoryFormats(t *testing.T) {
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
	for _, tc := range []struct {
		format, encoder, codec string
		extra                  []string
	}{
		{"mp3", "libmp3lame", "mp3", nil},
		{"flac", "flac", "flac", nil},
		{"ogg", "libvorbis", "vorbis", nil},
		{"opus", "libopus", "opus", nil},
		{"wav", "pcm_s16le", "pcm_s16le", nil},
		{"adts", "aac", "aac", nil},
		{"mp4", "aac", "aac", []string{"-movflags", "frag_keyframe+empty_moov"}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			args := []string{
				"-v", "error", "-f", "lavfi", "-i",
				"sine=frequency=440:sample_rate=48000:duration=0.3",
				"-c:a", tc.encoder,
			}
			args = append(args, tc.extra...)
			args = append(args, "-f", tc.format, "pipe:1")
			// И тестовые аудиоданные генерируются в stdout, без файлов.
			data, err := exec.CommandContext(ctx, ffmpeg, args...).Output()
			if err != nil {
				t.Fatalf("generate %s: %v", tc.format, err)
			}
			if tc.format == "flac" {
				// stdout не позволяет ffmpeg обновить STREAMINFO после кодирования.
				// Дополняем тестовый заголовок известным числом PCM-сэмплов:
				// 48000 Гц * 0.3 секунды. Аудиобайты остаются в памяти.
				if len(data) < 26 || string(data[:4]) != "fLaC" {
					t.Fatal("invalid FLAC fixture")
				}
				const samplesMask uint64 = 1<<36 - 1
				info := binary.BigEndian.Uint64(data[18:26])
				binary.BigEndian.PutUint64(data[18:26], info & ^samplesMask | 14400)
			}
			got, err := prober.NewFprobe(ffprobe).Probe(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got.Codec != tc.codec ||
				got.DurationMS == nil ||
				*got.DurationMS <= 0 ||
				got.SampleRate <= 0 {
				t.Fatalf("unexpected probe: %+v", got)
			}
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("analysis created temporary files")
	}
}

// TestFfprobeCancellationAndInvalidInput проверяет отмену до запуска анализа и
// классификацию пустого или неподдерживаемого входа как ErrInvalidFile.
func TestFfprobeCancellationAndInvalidInput(t *testing.T) {
	p := prober.NewFprobe("")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Probe(ctx, bytes.NewReader([]byte("audio")), 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if _, err := p.Probe(
		context.Background(),
		bytes.NewReader(nil), 0); !errors.Is(err, prober.ErrInvalidFile) {
		t.Fatalf("accepted empty input: %v", err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	if _, err := p.Probe(
		context.Background(),
		bytes.NewReader([]byte("invalid audio")), 13); !errors.Is(err, prober.ErrInvalidFile) {
		t.Fatalf("incorrect invalid file error: %v", err)
	}
}

// blockingInput служит источником, который сигнализирует о начале первого чтения и ждёт
// отмены контекста.
type blockingInput struct {
	ctx   context.Context
	ready chan struct{}
	once  sync.Once
}

// ReadAt сигнализирует о первом чтении и возвращает ошибку контекста после отмены.
func (b *blockingInput) ReadAt([]byte, int64) (int, error) {
	b.once.Do(func() { close(b.ready) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

// TestFfprobeCancellationDuringRead проверяет, что отмена во время чтения источника
// завершает Probe с context.Canceled.
func TestFfprobeCancellationDuringRead(t *testing.T) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &blockingInput{ctx: ctx, ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := prober.NewFprobe(ffprobe).Probe(ctx, src, 128000)
		done <- err
	}()
	select {
	case <-src.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("ffprobe did not start reading")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost active cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ffprobe survived cancellation")
	}
}
