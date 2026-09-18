package main

import (
	"log/slog"
	"os"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/app"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/lib/logger"
)

// @title           Somnium Shade Cast API
// @version         1.0
// @description     API Somnium Shade Cast
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apiKey CookieAuth
// @in                cookie
// @name              session_id
func main() {
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

	// Создаем сервер
	server, err := app.NewApp(cfg, log)
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
