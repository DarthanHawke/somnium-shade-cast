// Пакет app реализует создание и настройку компонентов сервиса.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/DarthanHawke/somnium-shade-cast/server/docs"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/metadata"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/prober"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/analysis/tags"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/middleware"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/config"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/streaming"
)

// App владеет ресурсами HTTP-сервера и фонового воркера.
type App struct {
	logger    *slog.Logger
	database  *sqlite.Database
	http      *http.Server
	worker    *jobs.Worker
	store     *storage.BlobStore
	storeLock *os.File
}

// NewApp создаёт компоненты сервера и блокирует каталог хранилища.
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

	ctx := context.Background()
	if cfg.Env == config.EnvProd {
		if cfg.AdminName == "" || strings.TrimSpace(cfg.AdminPassword) == "" {
			return errors.New("set SSC_ADMIN_NAME and SSC_ADMIN_PASSWORD for prod")
		}
	}
	if err := app.openData(ctx, cfg, mKey); err != nil {
		return err
	}
	db, store := app.database, app.store

	// Временное хранилище загружаемых файлов
	spool, err := storage.NewSpool(cfg.Scratch.Size)
	if err != nil {
		return fmt.Errorf("open upload spool: %w", err)
	}

	users := sqlite.NewUserRepository(db)
	dev := cfg.Env == config.EnvDev
	var auth func(http.Handler) http.Handler
	if dev {
		auth = middleware.DevAuth(users, log)
	} else {
		admin, err := users.GetByName(ctx, cfg.AdminName)
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("admin %q is missing: run init-admin first: %w", cfg.AdminName, err)
		}
		if err != nil {
			return fmt.Errorf("load prod admin: %w", err)
		}
		if admin.Role != domain.RoleAdmin || !admin.IsActive {
			return fmt.Errorf("account %q must be an active admin", cfg.AdminName)
		}
		auth = middleware.AdminAuth(users, admin.PublicID, admin.Name, cfg.AdminPassword, log)
	}
	tx := sqlite.NewTxRepository(db)
	tracks := sqlite.NewTrackRepository(db)
	albums := sqlite.NewAlbumRepository(db)
	artists := sqlite.NewArtistRepository(db)
	search := sqlite.NewSearchRepository(db)
	blobs := sqlite.NewBlobRepository(db)
	library := sqlite.NewLibraryRepository(db)
	jobRepo := sqlite.NewJobRepository(db)
	uploads := sqlite.NewUploadRepository(db)
	queue := jobs.New(jobRepo)

	proberSvc := prober.NewFFprobe(cfg.FfProber.Path)
	tagsSvc := tags.NewParser()

	metadataSvc := metadata.New(
		artists,
		albums,
		proberSvc,
		tagsSvc,
		log,
	)

	ingestSvc := ingest.New(ingest.Dependencies{
		Store:    store,
		Spool:    spool,
		Tx:       tx,
		Blobs:    blobs,
		Tracks:   tracks,
		Album:    albums,
		Search:   search,
		Metadata: metadataSvc,
		Uploads:  uploads,
		Queue:    queue,
	})

	collector := storage.NewCollector(store, blobs, log)
	worker, err := jobs.NewWorker(jobRepo, log, jobs.WorkerConfig{
		Parallelism:  2,
		PollInterval: 2 * time.Second,
		Lease:        30 * time.Second,
	}, map[domain.JobType]jobs.Registration{
		domain.JobCollectBlobs: {
			Handle: func(ctx context.Context, _ json.RawMessage) error {
				return collector.Collect(ctx, 30*time.Minute)
			},
			Timeout: 45 * time.Minute,
		},
		domain.JobPruneFinished: {
			Handle: func(ctx context.Context, _ json.RawMessage) error {
				const retention = 30 * 24 * time.Hour
				const batchSize = 1000
				cutoff := time.Now().Add(-retention)
				for {
					count, err := jobRepo.PruneFinished(ctx, cutoff, batchSize)
					if err != nil || count < batchSize {
						return err
					}
				}
			},
			Timeout: time.Minute,
		},
		domain.JobUploadTrack: {
			Handle:  ingestSvc.Process,
			Timeout: 2 * time.Minute,
		},
	})
	if err != nil {
		return fmt.Errorf("configure background jobs: %w", err)
	}
	if err := queue.EnsurePeriodic(context.Background(), domain.JobCollectBlobs,
		"storage", time.Hour, struct{}{}, jobs.WithMaxAttempts(3)); err != nil {
		return fmt.Errorf("register blob collection: %w", err)
	}
	if err := queue.EnsurePeriodic(context.Background(), domain.JobPruneFinished,
		"job-history", 24*time.Hour, struct{}{}, jobs.WithMaxAttempts(3)); err != nil {
		return fmt.Errorf("register job history cleanup: %w", err)
	}
	app.worker = worker

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

	router := handlers.Routes(auth, middleware.Limit(2, 8), dev)

	app.http = &http.Server{
		Addr: net.JoinHostPort(
			cfg.Server.Host,
			strconv.Itoa(cfg.Server.Port),
		),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		IdleTimeout:       time.Minute,
	}

	return nil
}

// openData блокирует каталог блобов, открывает SQLite и применяет миграции.
func (app *App) openData(ctx context.Context, cfg *config.Config, mKey []byte) error {
	log := app.logger

	keyManager, err := storage.NewKeyManager("default", mKey)
	if err != nil {
		return fmt.Errorf("init key manager: %w", err)
	}

	log.Info("key manager initialized")

	store, err := storage.NewBlobStore(cfg.Storage.Path, keyManager)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}

	lock, err := store.LockRoot()
	if err != nil {
		return fmt.Errorf("lock storage: %w", err)
	}
	app.store = store
	app.storeLock = lock
	log.Info("storage opened")

	dbDir := filepath.Dir(cfg.Database.Path)
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		return fmt.Errorf("create database directory %q: %w", dbDir, err)
	}

	db, err := sqlite.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	app.database = db

	log.Info("database connected")

	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	log.Info("database schema is up to date")

	return nil
}

// InitAdmin создаёт указанного в конфигурации администратора с остановленным сервером.
// Существующая учётная запись сохраняет свой публичный идентификатор.
func InitAdmin(ctx context.Context, cfg *config.Config, log *slog.Logger, mKey []byte) error {
	if cfg.AdminName == "" {
		return errors.New("set SSC_ADMIN_NAME before running init-admin")
	}
	resources := &App{logger: log}
	defer resources.shutdown()

	if err := resources.openData(ctx, cfg, mKey); err != nil {
		return err
	}
	admin, err := sqlite.NewUserRepository(resources.database).EnsureAdmin(ctx, cfg.AdminName)
	if err != nil {
		return fmt.Errorf("initialize admin: %w", err)
	}
	log.Info("admin account is provisioned", "name", admin.Name, "public_id", admin.PublicID, "active", admin.IsActive)
	return nil
}

// Run обслуживает HTTP и фоновые задачи до ошибки сервера или сигнала остановки.
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

	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		app.worker.Run(workerCtx)
	}()

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

	// Останавливаем HTTP и воркер до закрытия общих ресурсов приложения.
	cancelWorker()
	shutdownErr := app.stopHTTPServer()
	<-workerDone

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

// shutdown закрывает БД и освобождает блокировку хранилища.
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

	if app.storeLock != nil {
		if err := app.store.UnlockRoot(app.storeLock); err != nil {
			app.logger.Error("failed to release storage lock", "error", err)
		}
		app.storeLock = nil
	}

	app.logger.Info("resources released")
}
