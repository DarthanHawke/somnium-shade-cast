// Package logger настраивает вывод slog в консоль и файл с ротацией.
package logger

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
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

// New создаёт логгер и проверяет доступ к файлу, если включена запись на диск.
func New(cfg Config) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: cfg.Level, AddSource: cfg.AddSource}
	var stdout slog.Handler
	if cfg.Env == config.EnvDev {
		opts.Level = slog.LevelDebug
		stdout = slog.NewTextHandler(os.Stdout, opts)
	} else {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
	}

	if cfg.LogFile == "" {
		return slog.New(stdout), nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(cfg.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close log file after startup check: %w", err)
	}
	rotator := &lumberjack.Logger{
		Filename:   cfg.LogFile,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   true,
	}
	return slog.New(slog.NewMultiHandler(stdout, slog.NewJSONHandler(rotator, opts))), nil
}
