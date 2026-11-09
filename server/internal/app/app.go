// Пакет app реализует создание и настройку компонентов сервиса.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/DarthanHawke/somnium-shade-cast/server/docs"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/metadata"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/tags"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/streaming"
)

type App struct {
	logger   *slog.Logger
	database *sqlite.Database
	http     *http.Server
	router   http.Handler
	users    *sqlite.UserRepository
}

func NewApp(
	cfg *config.Config,
	log *slog.Logger,
	mKey []byte,
) (*App, error) {
	app := &App{
		logger: log,
	}

	if err := app.initialize(cfg, mKey); err != nil {
		app.shutdown()
		return nil, err
	}

	return app, nil
}

func (app *App) initialize(cfg *config.Config, mKey []byte) error {
	log := app.logger

	log.Info("initializing server", "port", cfg.Server.Port)

	db, err := sqlite.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	app.database = db

	log.Info("database connected")

	if err := db.Migrate(context.Background()); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	log.Info("database schema is up to date")

	keyManager, err := storage.NewKeyManager("default", mKey)
	if err != nil {
		return fmt.Errorf("init key manager: %w", err)
	}

	log.Info("key manager initialized")

	store, err := storage.NewBlobStore(cfg.Storage.Path, keyManager)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	log.Info("storage opened")

	// Временное хранилище загружаемых файлов
	spool, err := storage.NewSpool(cfg.Scratch.Size)
	if err != nil {
		return fmt.Errorf("open upload spool: %w", err)
	}

	users := sqlite.NewUserRepository(db)
	tx := sqlite.NewTxRepository(db)
	tracks := sqlite.NewTrackRepository(db)
	albums := sqlite.NewAlbumRepository(db)
	artists := sqlite.NewArtistRepository(db)
	search := sqlite.NewSearchRepository(db)
	blobs := sqlite.NewBlobRepository(db)
	library := sqlite.NewLibraryRepository(db)

	app.users = users

	proberSvc := prober.NewFprobe(cfg.FfProber.Path)
	tagsSvc := tags.NewParser()

	metadataSvc := metadata.New(
		artists,
		albums,
		proberSvc,
		tagsSvc,
		log,
	)

	jobsSvc := jobs.NewJobs()

	ingestSvc := ingest.New(ingest.Dependencies{
		Store:    store,
		Spool:    spool,
		Tx:       tx,
		Blobs:    blobs,
		Tracks:   tracks,
		Search:   search,
		Artists:  artists,
		Albums:   albums,
		Jobs:     jobsSvc,
		Metadata: metadataSvc,
		Logger:   log,
	})

	streamingSvc := streaming.New(
		tracks,
		library,
		blobs,
		store,
	)

	userHandler := api.NewUserHandler(users, log)
	ingestHandler := api.NewIngestHandler(ingestSvc, log)
	streamHandler := api.NewStreamHandler(streamingSvc, log)
	libraryHandler := api.NewLibraryHandler(library, log)

	handlers := api.NewHandlers(
		ingestHandler,
		libraryHandler,
		streamHandler,
		userHandler,
	)

	auth := middleware.DevAuth(users, true, log)
	limit := middleware.Limit(2, 8)

	app.router = handlers.Routes(auth, limit)

	app.http = &http.Server{
		Addr: net.JoinHostPort(
			cfg.Server.Host,
			strconv.Itoa(cfg.Server.Port),
		),
		Handler: app.router,
	}

	return nil
}

func (app *App) Run() error {
	defer app.shutdown()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	listener, err := net.Listen("tcp", app.http.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP %q: %w", app.http.Addr, err)
	}
	defer listener.Close()

	serveErrors := make(chan error, 1)

	go func() {
		serveErrors <- app.http.Serve(listener)
	}()

	app.logger.Info(
		"HTTP listener opened",
		"addr", listener.Addr().String(),
	)

	var serveErr error
	serveFinished := false

	select {
	case serveErr = <-serveErrors:
		serveFinished = true

	case <-ctx.Done():
		stop()
		app.logger.Info("received shutdown signal")
	}

	// Сначала прекращаем обработку HTTP, затем defer
	shutdownErr := app.stopHTTPServer()

	if !serveFinished {
		serveErr = <-serveErrors
	}

	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	if serveErr != nil {
		serveErr = fmt.Errorf("serve HTTP: %w", serveErr)
	}

	if err := errors.Join(serveErr, shutdownErr); err != nil {
		return err
	}

	app.logger.Info("HTTP server stopped")

	return nil
}

func (app *App) stopHTTPServer() error {
	if app.http == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := app.http.Shutdown(ctx); err != nil {
		closeErr := app.http.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("force close HTTP: %w", closeErr)
		}

		return errors.Join(
			fmt.Errorf("shutdown HTTP: %w", err),
			closeErr,
		)
	}

	return nil
}

// shutdown освобождает ресурсы приложения
func (app *App) shutdown() {
	app.logger.Info("shutting down resources")

	if app.database != nil {
		if err := app.database.Close(); err != nil {
			app.logger.Error(
				"failed to close database",
				"error", err,
			)
		}
	}

	app.logger.Info("resources released")
}
