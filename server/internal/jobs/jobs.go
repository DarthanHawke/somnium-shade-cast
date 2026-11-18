// Package jobs ставит задачи в устойчивую очередь и запускает фоновые обработчики.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// Queue сохраняет подготовленные задачи в хранилище.
type Queue interface {
	// Insert возвращает ID новой задачи или ноль при дедупликации.
	Insert(ctx context.Context, job domain.Job) (domain.ID, error)
}

// Jobs управляет постановкой задач в очередь.
type Jobs struct {
	queue Queue
}

// New создаёт очередь для прикладного кода.
func New(queue Queue) *Jobs {
	return &Jobs{queue: queue}
}

// Option настраивает постановку задачи.
type Option func(*enqueueOptions)

type enqueueOptions struct {
	priority    int
	maxAttempts int
	runAfter    time.Time
	dedupKey    string
}

// WithPriority задаёт приоритет. Большие значения исполняются раньше.
func WithPriority(priority int) Option {
	return func(opts *enqueueOptions) { opts.priority = priority }
}

// WithRunAfter задаёт самое раннее время первого запуска.
func WithRunAfter(at time.Time) Option {
	return func(opts *enqueueOptions) { opts.runAfter = at }
}

// WithDedupKey задаёт ключ среди незавершённых задач того же типа.
func WithDedupKey(key string) Option {
	return func(opts *enqueueOptions) { opts.dedupKey = key }
}

// WithMaxAttempts задаёт максимальное число попыток, включая первую.
func WithMaxAttempts(n int) Option {
	return func(opts *enqueueOptions) { opts.maxAttempts = n }
}

// Enqueue сохраняет задачу в транзакции из ctx и возвращает её внутренний ID.
// Ноль означает, что запрос покрывает существующий ключ дедупликации.
func (j *Jobs) Enqueue(ctx context.Context, kind domain.JobType, payload any, opts ...Option) (domain.ID, error) {
	return j.enqueue(ctx, kind, payload, "", 0, opts)
}

// EnsurePeriodic создаёт повторяющуюся задачу с указанным ключом. Каждая серия
// складывается из одной или нескольких попыток; интервал отсчитывается
// от завершения серии. Повторный вызов сохраняет существующее расписание.
func (j *Jobs) EnsurePeriodic(
	ctx context.Context,
	kind domain.JobType,
	key string,
	every time.Duration,
	payload any,
	opts ...Option,
) error {
	if key == "" || every < time.Millisecond || every%time.Millisecond != 0 {
		return errors.New("jobs: periodic job requires a key and a positive interval in whole milliseconds")
	}
	_, err := j.enqueue(ctx, kind, payload, key, every, opts)
	return err
}

func (j *Jobs) enqueue(
	ctx context.Context,
	kind domain.JobType,
	payload any,
	key string,
	every time.Duration,
	opts []Option,
) (domain.ID, error) {
	settings := enqueueOptions{
		priority:    5,
		maxAttempts: 5,
		runAfter:    time.Now(),
	}
	for _, apply := range opts {
		apply(&settings)
	}
	// Расписание имеет собственный ключ. Обычный Enqueue использует ключ из opts.
	if every > 0 {
		settings.dedupKey = key
	}
	if kind == "" || settings.maxAttempts < 1 {
		return 0, errors.New("jobs: job type and at least one attempt are required")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("jobs: encode %s payload: %w", kind, err)
	}

	return j.queue.Insert(ctx, domain.Job{
		Type:        kind,
		Payload:     string(data),
		Priority:    settings.priority,
		MaxAttempts: settings.maxAttempts,
		RunAfter:    domain.UnixTimeMillis(settings.runAfter),
		DedupKey:    settings.dedupKey,
		Every:       every,
	})
}
