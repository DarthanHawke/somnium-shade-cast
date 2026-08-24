// Пакет app реализовывает создание и настройку всех компонентов сервиса
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	database "github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"

	"github.com/go-chi/chi/v5"
)

// App управляет всеми компонентами сервиса
type App struct {
	logger   *slog.Logger
	database *database.Database
	http     *http.Server
	router   *chi.Mux
}

// NewApp создает все компоненты и настраивает зависимости
func NewApp(cfg *config.Config, log *slog.Logger) (*App, error) {
	log.Info("initializing server", "port", cfg.Server.Port)

	// Подключаемся к бд
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	log.Info("database connected")

	// открываем http сервер
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	router := chi.NewRouter()
	http := &http.Server{
		Addr:    addr,
		Handler: router,
	}
	app := App{
		logger:   log,
		database: db,
		http:     http,
		router:   router,
	}
	app.routes()

	return &app, nil
}

// Run запускает все компоненты сервера
func (app *App) Run() error {
	defer app.shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.logger.Info("run all servers and connections")

	// Запускаем http сервер для prometheus
	go app.runHTTPServer()
	app.logger.Info("server started successfully")

	// Ждем сигнал для graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		app.logger.Info("received shutdown signal")
	case <-ctx.Done():
		app.logger.Info("context cancelled")
	}

	app.stopHTTPServer()
	app.logger.Info("http server stopped")
	app.logger.Info("server stopped gracefully")
	return nil
}

// закрываем все соединения
func (app *App) shutdown() {
	app.logger.Info("shutting down resources")

	if app.database != nil {
		if err := app.database.Close(); err != nil {
			app.logger.Error("failed to close database", "error", err)
		}
	}

	app.logger.Info("resources released")
}

// runHTTPServer - заускаем http сервер
func (app *App) runHTTPServer() {
	app.logger.Info("starting server", "port", app.http.Addr)
	if err := app.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		app.logger.Error("server failed", "error", err)
	}
}

// stopHTTPServer - отсанавливаем http сервер
func (app *App) stopHTTPServer() {
	if app.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.http.Shutdown(ctx); err != nil {
			app.logger.Error("failed to shutdown server", "error", err)
		}
	}
}

// routes регестрирует эндпоинты
func (app *App) routes() {
	// Health-check для мониторинга
	app.router.Get("/health", app.handleHealth())

	// Версия API v1 — базовый роут
	app.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", app.handlePing())
		r.Get("/version", app.handleVersion())

	})
}

// handleHealth возвращает статус сервиса
func (app *App) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"status":  "ok",
			"version": "0.1.0",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// handlePing — тестовый эндпоинт
func (app *App) handlePing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{
			"message": "pong",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// handleVersion — тестовый эндпоинт
func (app *App) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := map[string]string{
			"version": "0.1.0",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}
