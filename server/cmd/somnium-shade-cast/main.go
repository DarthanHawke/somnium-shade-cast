package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/app"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/lib/logger"
	"github.com/joho/godotenv"
)

// @title           Somnium Shade Cast API
// @version         1.0
// @description     API Somnium Shade Cast
// @BasePath        /
// @securityDefinitions.apikey DevAuth
// @in header
// @name X-User
// @description Публичный ULID активного пользователя для режима dev.
// @securityDefinitions.basic BasicAuth
// @description Тестовая учётная запись администратора для режима prod.
func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	initAdmin := len(args) > 0 && args[0] == "init-admin"
	if initAdmin {
		args = args[1:]
	}
	cfg, err := loadConfig(args)
	if err != nil {
		return err
	}
	mKey, err := loadMasterKey()
	if err != nil {
		return err
	}

	log, err := logger.New(logger.Config{
		Env:        cfg.Env,
		Level:      slog.LevelInfo,
		LogFile:    cfg.Logs.LogFile,
		MaxSize:    cfg.Logs.MaxSize,
		MaxBackups: cfg.Logs.MaxBackups,
		MaxAge:     cfg.Logs.MaxAge,
		AddSource:  cfg.Logs.AddSource,
	})

	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}

	if initAdmin {
		return app.InitAdmin(context.Background(), cfg, log, mKey)
	}

	server, err := app.NewApp(cfg, log, mKey)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}
	if err := server.Run(); err != nil {
		return fmt.Errorf("run server: %w", err)
	}
	return nil
}

func loadConfig(args []string) (*config.Config, error) {
	flags := flag.NewFlagSet("somnium-shade-cast", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: somnium-shade-cast [init-admin] [--config path]")
		flags.PrintDefaults()
	}
	configPath := flags.String("config", "", "configuration file (overrides SSC_CONFIG)")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("unexpected positional arguments")
	}

	if err := loadDotEnv(".env"); err != nil {
		return nil, err
	}
	if *configPath == "" {
		*configPath = os.Getenv("SSC_CONFIG")
	}
	return config.Load(*configPath)
}

// loadDotEnv подставляет значения локального файла в ещё не заданные переменные.
// Общая диагностика синтаксиса защищает значения из повреждённой строки от вывода в лог.
func loadDotEnv(path string) error {
	err := godotenv.Load(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if _, ok := errors.AsType[*os.PathError](err); ok {
		return fmt.Errorf("read environment file: %w", err)
	}
	return fmt.Errorf("invalid environment file %q", path)
}

// loadMasterKey читает Base64-представление 32 байт из окружения сервера.
func loadMasterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("SSC_MASTER_KEY"))
	if raw == "" {
		return nil, errors.New("SSC_MASTER_KEY is required")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, errors.New("SSC_MASTER_KEY must be standard Base64 encoding of 32 bytes")
	}
	return key, nil
}
