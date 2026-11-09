package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/app"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/lib/logger"
	"github.com/joho/godotenv"
)

// @title           Somnium Shade Cast API
// @version         1.0
// @description     API Somnium Shade Cast
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey DevAuth
// @in header
// @name X-User
// @description Публичный ULID активного пользователя для локального режима разработки.
func main() {
	// Загружаем env
	if err := godotenv.Load(); err != nil {
		slog.Error("Failed to load env", "error", err)
		os.Exit(1)
	}
	// Загружаем конфиг
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	log := logger.New(logger.Config{
		Level:      slog.LevelInfo,
		LogFile:    cfg.Logs.LogFile,
		MaxSize:    cfg.Logs.MaxSize,
		MaxBackups: cfg.Logs.MaxBackups,
		MaxAge:     cfg.Logs.MaxAge,
		AddSource:  cfg.Logs.AddSource,
	})

	mKey, err := loadMasterKey()
	if err != nil {
		log.Error("Failed to get master key", "error", err)
		os.Exit(1)
	}

	// Создаем сервер
	server, err := app.NewApp(cfg, log, mKey)
	if err != nil {
		log.Error("Failed to create server", "error", err)
		os.Exit(1)
	}

	// Запускаем сервер
	if err := server.Run(); err != nil {
		log.Error("Server error", "error", err)
		os.Exit(1)
	}
}

func loadMasterKey() ([]byte, error) {
	raw := os.Getenv("SSC_MASTER_KEY")
	if raw != "" {
		// TODO валидация и возврат
	}
	// Fallback если dev
	if os.Getenv("SSC_ENV") == "dev" {
		return []byte("0123456789abcdef0123456789abcdef"), nil
	}
	return nil, fmt.Errorf("SSC_MASTER_KEY is required")
}
