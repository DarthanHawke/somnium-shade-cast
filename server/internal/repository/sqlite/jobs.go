package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

// JobRepository хранит очередь и атомарно меняет состояние арендованных задач.
type JobRepository struct {
	db *Database
}

// NewJobRepository создаёт репозиторий очереди.
func NewJobRepository(db *Database) *JobRepository {
	return &JobRepository{db: db}
}

// Время аренды берём в SQL, когда операция получила доступ к БД.
const jobNow = "CAST(round(unixepoch('subsec') * 1000) AS INTEGER)"

// Insert создаёт задачу и возвращает её ID или ноль при дедупликации.
func (r *JobRepository) Insert(ctx context.Context, job domain.Job) (domain.ID, error) {
	const query = `
		INSERT INTO jobs (type, payload, priority, max_attempts, run_after, dedup_key, run_every_ms)
		VALUES (?1, ?2, ?3, ?4, ?5, NULLIF(?6, ''), ?7)
		ON CONFLICT(type, dedup_key)
		    WHERE dedup_key IS NOT NULL AND status IN ('pending', 'failed', 'running')
		    DO NOTHING
		RETURNING id`
	var id domain.ID
	err := r.db.executor(ctx).GetContext(ctx, &id, query,
		job.Type, job.Payload, job.Priority, job.MaxAttempts, job.RunAfter, job.DedupKey, job.Every.Milliseconds())
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, wrapDBError("jobs.Insert", err)
}

// Claim возвращает готовую задачу одного из types. Больший приоритет обрабатывается
// раньше, при равном приоритете первыми идут задачи с ближайшим run_after.
func (r *JobRepository) Claim(ctx context.Context, types []domain.JobType, lease time.Duration) (*domain.Job, error) {
	const op = "jobs.Claim"

	if len(types) == 0 {
		return nil, nil
	}
	encodedTypes, err := json.Marshal(types)
	if err != nil {
		return nil, fmt.Errorf("%s: encode job types: %w", op, err)
	}
	typeList := string(encodedTypes)
	exec := r.db.executor(ctx)

	// Истёкшая аренда расходует попытку. Завершаем исчерпанные серии,
	// освобождая ключ обычной задачи или назначая очередной периодический запуск.
	const recoverExhausted = `
		UPDATE jobs
		SET status = CASE WHEN run_every_ms > 0 THEN 'pending' ELSE 'dead' END,
		    run_after = CASE WHEN run_every_ms > 0 THEN ` + jobNow + ` + run_every_ms ELSE run_after END,
		    attempts = CASE WHEN run_every_ms > 0 THEN 0 ELSE attempts END,
		    last_error = 'job lease expired',
		    lease_until = NULL, lease_token = NULL, finished_at = ` + jobNow + `
		WHERE status = 'running' AND COALESCE(lease_until, 0) <= ` + jobNow + `
		    AND attempts >= max_attempts AND type IN (SELECT value FROM json_each(?1))`
	if _, err := exec.ExecContext(ctx, recoverExhausted, typeList); err != nil {
		return nil, wrapDBError(op+" recover", err)
	}

	const query = `
		UPDATE jobs
		SET status = 'running', attempts = attempts + 1, started_at = ` + jobNow + `,
		    finished_at = NULL, lease_token = ?2, lease_until = ` + jobNow + ` + ?3
		WHERE id = (
		    SELECT id FROM jobs
		    WHERE type IN (SELECT value FROM json_each(?1)) AND attempts < max_attempts
		      AND (
		          (status IN ('pending', 'failed') AND run_after <= ` + jobNow + `)
		          OR (status = 'running' AND COALESCE(lease_until, 0) <= ` + jobNow + `)
		      )
		    ORDER BY priority DESC, run_after, id
		    LIMIT 1
		)
		RETURNING id, type, payload, attempts, max_attempts, lease_token`

	var job domain.Job
	err = exec.QueryRowxContext(ctx, query, typeList, string(domain.NewPublicID()), lease.Milliseconds()).
		Scan(&job.ID, &job.Type, &job.Payload, &job.Attempts, &job.MaxAttempts, &job.LeaseToken)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, wrapDBError(op, err)
	default:
		return &job, nil
	}
}

// Renew продлевает только действующую аренду. Истёкший токен восстановить нельзя.
func (r *JobRepository) Renew(ctx context.Context, job domain.Job, lease time.Duration) (bool, error) {
	const query = `
		UPDATE jobs SET lease_until = ` + jobNow + ` + ?3
		WHERE id = ?1 AND lease_token = ?2 AND status = 'running' AND lease_until > ` + jobNow
	return execAffected(ctx, r.db.executor(ctx), "jobs.Renew", query, job.ID, job.LeaseToken, lease.Milliseconds())
}

// Finish сохраняет результат действующей аренды. Повторяемая ошибка сохраняет ключ
// и счётчик попыток; завершение серии периодической задачи назначает новый запуск.
func (r *JobRepository) Finish(ctx context.Context, job domain.Job, errorText string, retryAfter time.Duration) (bool, error) {
	const query = `
		UPDATE jobs
		SET status = CASE
		        WHEN ?4 > 0 THEN 'failed'
		        WHEN run_every_ms > 0 THEN 'pending'
		        WHEN ?3 = '' THEN 'done'
		        ELSE 'dead'
		    END,
		    run_after = CASE
		        WHEN ?4 > 0 THEN ` + jobNow + ` + ?4
		        WHEN run_every_ms > 0 THEN ` + jobNow + ` + run_every_ms
		        ELSE run_after
		    END,
		    attempts = CASE WHEN run_every_ms > 0 AND ?4 = 0 THEN 0 ELSE attempts END,
		    last_error = ?3, finished_at = ` + jobNow + `, lease_token = NULL, lease_until = NULL
		WHERE id = ?1 AND lease_token = ?2 AND status = 'running' AND lease_until > ` + jobNow
	return execAffected(ctx, r.db.executor(ctx), "jobs.Finish", query,
		job.ID, job.LeaseToken, errorText, retryAfter.Milliseconds())
}

// Release снимает действующую аренду и возвращает незавершённую попытку в очередь.
func (r *JobRepository) Release(ctx context.Context, job domain.Job) (bool, error) {
	const query = `
		UPDATE jobs SET status = 'pending', attempts = attempts - 1,
		    lease_token = NULL, lease_until = NULL
		WHERE id = ?1 AND lease_token = ?2 AND status = 'running' AND lease_until > ` + jobNow
	return execAffected(ctx, r.db.executor(ctx), "jobs.Release", query, job.ID, job.LeaseToken)
}

// PruneFinished удаляет до limit завершённых задач старше before.
// История загрузок хранится отдельно в track_uploads.
func (r *JobRepository) PruneFinished(ctx context.Context, before time.Time, limit int) (int64, error) {
	const op = "jobs.PruneFinished"
	if limit <= 0 {
		return 0, fmt.Errorf("%s: limit must be positive", op)
	}
	const query = `
		DELETE FROM jobs
		WHERE id IN (
			SELECT j.id FROM jobs AS j
			WHERE j.status IN ('done', 'dead') AND j.finished_at < ?1
			    AND NOT EXISTS (SELECT 1 FROM track_uploads AS u WHERE u.job_id = j.id)
			ORDER BY j.finished_at, j.id
			LIMIT ?2
		)`
	result, err := r.db.executor(ctx).ExecContext(ctx, query, before.UnixMilli(), limit)
	if err != nil {
		return 0, wrapDBError(op, err)
	}
	count, err := result.RowsAffected()
	return count, wrapDBError(op, err)
}

func execAffected(ctx context.Context, exec executor, op, query string, args ...any) (bool, error) {
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return false, wrapDBError(op, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, wrapDBError(op, err)
	}
	return affected != 0, nil
}
