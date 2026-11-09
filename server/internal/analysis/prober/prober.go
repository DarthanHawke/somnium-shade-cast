// Package prober извлекает технические характеристики аудио с помощью ffprobe.
package prober

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FFprobe извлекает технические характеристики из доступного для чтения источника.
type FFprobe struct {
	ffprobePath string // путь к бинарнику; если пусто — ищется в PATH
}

// NewFFprobe создаёт анализатор. Если path пуст, ffprobe ищется в PATH.
func NewFprobe(ffprobePath string) *FFprobe {
	return &FFprobe{ffprobePath: ffprobePath}
}

// ProbeResult содержит характеристики первого аудиопотока и его контейнера.
type ProbeResult struct {
	Codec       string // "mp3", "flac", "aac", "ogg"
	MimeType    string // "audio/mpeg", "audio/flac", ...
	DurationMS  *int64
	BitrateKbps int
	SampleRate  int // Hz
	Channels    int
}

// ffprobeOutput описывает JSON-структуру, возвращаемую ffprobe
type ffprobeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// ffprobeStream описывает JSON-структуру аудиопотока, возвращаемую ffprobe
type ffprobeStream struct {
	CodecName  string `json:"codec_name"`
	CodecType  string `json:"codec_type"`
	SampleRate string `json:"sample_rate"`
	Channels   int    `json:"channels"`
	BitRate    string `json:"bit_rate"`
}

// Probe извлекает характеристики первого аудиопотока.
// Длительность может быть неизвестна. Метод учитывает отмену контекста.
func (p *FFprobe) Probe(ctx context.Context, src io.ReaderAt, size int64) (*ProbeResult, error) {
	const op = "prober.Probe"

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, fmt.Errorf("%w: %s: empty input", ErrInvalidFile, op)
	}

	bin := p.ffprobePath
	if bin == "" {
		var err error
		bin, err = exec.LookPath("ffprobe")
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrUnavailable, op, err)
		}
	}

	input, err := newProbeInput(ctx, src, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: start memory input: %v", ErrUnavailable, op, err)
	}
	defer input.Close()

	// -v error: только ошибки, без лишнего шума
	// -print_format json: вывод в JSON
	// -show_format -show_streams: минимально необходимое
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "format=format_name,duration,bit_rate:stream=codec_name,codec_type,sample_rate,channels,bit_rate",
		"-protocol_whitelist", "http,tcp",
		"-http_proxy", "",
		// Не принимаем плейлисты, способные ссылаться на внешние ресурсы.
		"-format_whitelist", "aac,aiff,ape,asf,flac,matroska,webm,mov,mp3,ogg,wav,wv",
		input.url,
	)

	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", op, ctxErr)
		}
		// ffprobe возвращает non-zero exit при ошибке парсинга
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s: %s", ErrInvalidFile, op, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrUnavailable, op, err)
	}

	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("%w: %s: parse output: %v", ErrUnavailable, op, err)
	}

	// Находим аудиопоток
	var audioStream *ffprobeStream
	for i := range raw.Streams {
		if raw.Streams[i].CodecType == "audio" {
			audioStream = &raw.Streams[i]
			break
		}
	}
	if audioStream == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotAudio, op)
	}

	// Парсим длительность
	durationMS, err := parseDurationMS(raw.Format.Duration)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Битрейт
	bitrateStr := raw.Format.BitRate
	if bitrateStr == "" {
		bitrateStr = audioStream.BitRate
	}
	bitrate, _ := strconv.Atoi(bitrateStr)
	bitrateKbps := bitrate / 1000

	sampleRate, _ := strconv.Atoi(audioStream.SampleRate)

	return &ProbeResult{
		Codec:       audioStream.CodecName,
		MimeType:    mimeFromFormat(raw.Format.FormatName),
		DurationMS:  durationMS,
		BitrateKbps: bitrateKbps,
		SampleRate:  sampleRate,
		Channels:    audioStream.Channels,
	}, nil
}

// parseDurationMS парсит длительность в секундах и возвращает миллисекунды.
func parseDurationMS(value string) (*int64, error) {
	if value == "" || value == "N/A" {
		return nil, nil
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid duration %q: %v", ErrUnavailable, value, err)
	}
	ms := seconds * 1000
	if math.IsNaN(ms) || ms < 0 || ms >= float64(math.MaxInt64) {
		return nil, fmt.Errorf("%w: invalid duration %q", ErrUnavailable, value)
	}
	durationMS := int64(ms)
	return &durationMS, nil
}

// mimeFromFormat определяет MIME по контейнеру
func mimeFromFormat(formatNames string) string {
	for _, format := range strings.Split(formatNames, ",") {
		switch format {
		case "mp3":
			return "audio/mpeg"
		case "flac":
			return "audio/flac"
		case "aac":
			return "audio/aac"
		case "ogg":
			return "audio/ogg"
		case "mov", "mp4", "m4a", "3gp", "3g2", "mj2":
			return "audio/mp4"
		case "wav":
			return "audio/wav"
		case "aiff":
			return "audio/aiff"
		}
	}
	return "application/octet-stream"
}
