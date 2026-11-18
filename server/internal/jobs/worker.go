package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// Repository реализует выдачу и завершение задач с ограниченной по времени арендой.
type Repository interface {
	// Claim атомарно выдаёт готовую задачу одного из types. nil означает пустую очередь.
	Claim(ctx context.Context, types []domain.JobType, lease time.Duration) (*domain.Job, error)
	// Renew продлевает действующую аренду. false означает потерю права выполнять задачу.
	Renew(ctx context.Context, job domain.Job, lease time.Duration) (bool, error)
	// Finish записывает результат действующей аренды. errorText пуст при успехе.
	// retryAfter равен нулю, если серия попыток завершена.
	Finish(ctx context.Context, job domain.Job, errorText string, retryAfter time.Duration) (bool, error)
	// Release возвращает задачу в очередь после кооперативной остановки обработчика.
	Release(ctx context.Context, job domain.Job) (bool, error)
}

// Handler выполняет задачу. После истечения аренды или при сбое до сохранения
// результата задача может выполняться повторно. Обработчик должен учитывать
// повторное выполнение и прекращать работу при отмене ctx.
type Handler func(ctx context.Context, payload json.RawMessage) error

type attemptKey struct{}

// AttemptFromContext возвращает текущую задачу с токеном аренды.
// Токен нужно проверять в транзакции с изменениями, требующими действующей аренды.
func AttemptFromContext(ctx context.Context) (domain.Job, bool) {
	job, ok := ctx.Value(attemptKey{}).(domain.Job)
	return job, ok
}

// Registration задаёт обработчик и предельное время одной попытки.
type Registration struct {
	Handle  Handler
	Timeout time.Duration
}

// WorkerConfig задаёт параллелизм, интервал опроса и длительность аренды.
type WorkerConfig struct {
	Parallelism  int
	PollInterval time.Duration
	Lease        time.Duration
}

// Worker получает из очереди задачи зарегистрированных типов.
type Worker struct {
	repo     Repository
	log      *slog.Logger
	cfg      WorkerConfig
	handlers map[domain.JobType]Registration
	types    []domain.JobType
}

// NewWorker создаёт воркер с фиксированным набором обработчиков.
func NewWorker(
	repo Repository,
	log *slog.Logger,
	cfg WorkerConfig,
	handlers map[domain.JobType]Registration,
) (*Worker, error) {
	if cfg.Parallelism < 1 || cfg.PollInterval <= 0 || cfg.Lease < 3*time.Millisecond ||
		cfg.Lease%time.Millisecond != 0 || len(handlers) == 0 {
		return nil, errors.New("jobs: invalid worker configuration")
	}

	worker := &Worker{
		repo: repo, log: log, cfg: cfg,
		handlers: make(map[domain.JobType]Registration, len(handlers)),
		types:    make([]domain.JobType, 0, len(handlers)),
	}
	for kind, registration := range handlers {
		if kind == "" || registration.Handle == nil || registration.Timeout <= 0 {
			return nil, fmt.Errorf("jobs: invalid handler registration for %q", kind)
		}
		worker.handlers[kind] = registration
		worker.types = append(worker.types, kind)
	}
	return worker, nil
}

// Run опрашивает очередь до отмены ctx и ждёт завершения своих обработчиков.
func (w *Worker) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for range w.cfg.Parallelism {
		workers.Go(func() { w.run(ctx) })
	}
	workers.Wait()
}

func (w *Worker) run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := w.repo.Claim(ctx, w.types, w.cfg.Lease)
		if err == nil && job != nil {
			w.execute(ctx, *job)
			continue
		}
		if err != nil && ctx.Err() == nil {
			w.log.ErrorContext(ctx, "claim job", "error", err)
		}
		timer := time.NewTimer(w.cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) execute(ctx context.Context, job domain.Job) {
	registration := w.handlers[job.Type]
	attemptCtx, cancelAttempt := context.WithTimeout(ctx, registration.Timeout)
	defer cancelAttempt()

	heartbeatCtx, stopHeartbeat := context.WithCancel(attemptCtx)
	defer stopHeartbeat()
	leaseLost := make(chan bool, 1)
	go func() {
		lost := w.heartbeat(heartbeatCtx, job)
		if lost {
			cancelAttempt()
		}
		leaseLost <- lost
	}()

	handleCtx := context.WithValue(attemptCtx, attemptKey{}, job)
	handleErr := callHandler(handleCtx, registration.Handle, json.RawMessage(job.Payload))
	if err := attemptCtx.Err(); err != nil {
		handleErr = err
	}
	stopHeartbeat()
	if <-leaseLost {
		return
	}

	// Завершение аренды требуется и после отмены родительского контекста.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), w.cfg.Lease/3)
	defer cancelSave()

	var saved bool
	var err error
	if ctx.Err() != nil && handleErr != nil {
		saved, err = w.repo.Release(saveCtx, job)
	} else {
		var errorText string
		var retryAfter time.Duration
		if handleErr != nil {
			errorText = handleErr.Error()
			if errorText == "" {
				errorText = "handler failed"
			}
			if job.Attempts < job.MaxAttempts && !errors.Is(handleErr, errPermanent) {
				retryAfter = job.RetryDelay()
			}
			w.log.WarnContext(saveCtx, "job failed", "job_id", job.ID, "type", job.Type,
				"attempt", job.Attempts, "retry_after", retryAfter, "error", handleErr)
		}
		saved, err = w.repo.Finish(saveCtx, job, errorText, retryAfter)
	}

	switch {
	case err != nil:
		w.log.ErrorContext(saveCtx, "save job result", "job_id", job.ID, "type", job.Type, "error", err)
	case !saved:
		w.log.WarnContext(saveCtx, "job lease lost before saving result", "job_id", job.ID, "type", job.Type)
	}
}

func (w *Worker) heartbeat(ctx context.Context, job domain.Job) bool {
	interval := w.cfg.Lease / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			requestCtx, cancel := context.WithTimeout(ctx, interval)
			ok, err := w.repo.Renew(requestCtx, job, w.cfg.Lease)
			cancel()
			if ctx.Err() != nil {
				return false
			}
			if err != nil || !ok {
				w.log.WarnContext(ctx, "job lease renewal failed", "job_id", job.ID, "type", job.Type, "error", err)
				return true
			}
		}
	}
}

var errPermanent = errors.New("permanent job failure")

// Permanent завершает серию попыток при ошибке, которую повтор не исправит.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(errPermanent, err)
}

func callHandler(ctx context.Context, handle Handler, payload json.RawMessage) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("job handler panicked: %v", recovered)
		}
	}()
	return handle(ctx, payload)
}
