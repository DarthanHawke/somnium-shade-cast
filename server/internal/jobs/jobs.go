package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

type Jobs struct {
	enqueueOpts
}

func NewJobs() Jobs { return Jobs{} }

type Handler func(ctx context.Context, payload json.RawMessage) error

// Option - настройки постановки задачи.
type Option func(*enqueueOpts)

type enqueueOpts struct {
	priority int
	runAfter time.Time
	dedupKey string
	maxAtt   int
}

func WithPriority(p int) Option       { return func(o *enqueueOpts) { o.priority = p } }
func WithRunAfter(t time.Time) Option { return func(o *enqueueOpts) { o.runAfter = t } }
func WithDedupKey(k string) Option    { return func(o *enqueueOpts) { o.dedupKey = k } }
func WithMaxAttempts(n int) Option    { return func(o *enqueueOpts) { o.maxAtt = n } }

// Enqueue — заглушка
func (j Jobs) Enqueue(ctx context.Context, jobType domain.JobType, payload any, opts ...Option) error {

	return nil
}
