// Пакет logger предоставляет настроенный slog.Logger
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config содержит настройки логгера.
type Config struct {
	Env        string
	Level      slog.Level
	LogFile    string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	AddSource  bool
}

// New создает настроенный *slog.Logger.
func New(cfg Config) *slog.Logger {
	level := cfg.Level
	if cfg.Env != "production" {
		level = slog.LevelDebug
	}

	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if cfg.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0755); err != nil {
			os.Stderr.WriteString("logger: failed to create log directory: " + err.Error() + "\n")
		} else {
			rotator := &lumberjack.Logger{
				Filename:   cfg.LogFile,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				MaxAge:     cfg.MaxAge,
				Compress:   true, // gzip старые логи
			}
			writers = append(writers, rotator)
		}
	}

	writer := io.MultiWriter(writers...)

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:       level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: replaceAttr(),
	}

	if cfg.Env != "production" {
		handler = newDevHandler(writer, opts, cfg.LogFile != "")
	} else {
		handler = slog.NewJSONHandler(writer, opts)
	}

	handler = &stacktraceHandler{
		handler: handler,
		level:   slog.LevelError,
	}

	logger := slog.New(handler)

	return logger
}

type devHandler struct {
	stdoutHandler slog.Handler
	fileHandler   slog.Handler
	hasFile       bool
}

func newDevHandler(w io.Writer, opts *slog.HandlerOptions, hasFile bool) slog.Handler {
	h := &devHandler{hasFile: hasFile}

	// stdout — цветной текст
	stdoutOpts := *opts
	stdoutOpts.ReplaceAttr = nil // убираем кастомный replaceAttr для текстового формата
	h.stdoutHandler = NewPrettyHandler(os.Stdout, &stdoutOpts)

	if hasFile {
		// Файл — JSON
		h.fileHandler = slog.NewJSONHandler(w, opts)
	} else {
		// Файла нет — только stdout
		h.fileHandler = h.stdoutHandler
	}

	return h
}

func (h *devHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.stdoutHandler.Enabled(ctx, level)
}

func (h *devHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.stdoutHandler.Handle(ctx, r); err != nil {
		return err
	}
	if h.hasFile {
		return h.fileHandler.Handle(ctx, r)
	}
	return nil
}

func (h *devHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &devHandler{
		stdoutHandler: h.stdoutHandler.WithAttrs(attrs),
		fileHandler:   h.fileHandler.WithAttrs(attrs),
		hasFile:       h.hasFile,
	}
}

func (h *devHandler) WithGroup(name string) slog.Handler {
	return &devHandler{
		stdoutHandler: h.stdoutHandler.WithGroup(name),
		fileHandler:   h.fileHandler.WithGroup(name),
		hasFile:       h.hasFile,
	}
}

// stacktraceHandler добавляет stacktrace к логам уровня level и выше.
type stacktraceHandler struct {
	handler slog.Handler
	level   slog.Level
}

func (h *stacktraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *stacktraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= h.level {
		// Захватываем stacktrace текущей горутины
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		r.AddAttrs(slog.String("stacktrace", string(buf[:n])))
	}
	return h.handler.Handle(ctx, r)
}

func (h *stacktraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &stacktraceHandler{
		handler: h.handler.WithAttrs(attrs),
		level:   h.level,
	}
}

func (h *stacktraceHandler) WithGroup(name string) slog.Handler {
	return &stacktraceHandler{
		handler: h.handler.WithGroup(name),
		level:   h.level,
	}
}

// NewPrettyHandler создает handler с цветным выводом
func NewPrettyHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &prettyHandler{
		Handler: slog.NewTextHandler(w, opts),
		w:       w,
	}
}

type prettyHandler struct {
	slog.Handler
	w io.Writer
}

func (h *prettyHandler) Handle(ctx context.Context, r slog.Record) error {
	// Добавляем цвет к уровню
	level := r.Level.String()
	switch r.Level {
	case slog.LevelDebug:
		level = "\033[36mDEBUG\033[0m" // голубой
	case slog.LevelInfo:
		level = "\033[32mINFO\033[0m" // зеленый
	case slog.LevelWarn:
		level = "\033[33mWARN\033[0m" // желтый
	case slog.LevelError:
		level = "\033[31mERROR\033[0m" // красный
	}

	// Форматируем время
	timeStr := r.Time.Format("15:04:05.000")

	// Собираем атрибуты
	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key+"="+a.Value.String())
		return true
	})

	msg := timeStr + " " + level + " " + r.Message
	if len(attrs) > 0 {
		msg += " " + strings.Join(attrs, " ")
	}
	msg += "\n"

	h.w.Write([]byte(msg))
	return nil
}

func replaceAttr() func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		switch a.Key {
		case slog.TimeKey:
			// ISO-8601 с миллисекундами
			a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
		case slog.LevelKey:
			// Уровень в верхнем регистре
			a.Value = slog.StringValue(strings.ToUpper(a.Value.String()))
		case slog.MessageKey:
		}
		return a
	}
}
