package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// TestJobPriorityDedupAndRollback проверяет приоритет, фильтр обработчиков,
// дедупликацию отложенного повтора и совместный откат прикладной операции.
func TestJobPriorityDedupAndRollback(t *testing.T) {
	h := newRig(t)
	ctx := t.Context()
	repo := sqlite.NewJobRepository(h.db)
	queue := jobs.New(repo)
	kind := domain.JobCollectBlobs
	mustEnqueue := func(kind domain.JobType, opts ...jobs.Option) {
		t.Helper()
		if _, err := queue.Enqueue(ctx, kind, struct{}{}, opts...); err != nil {
			t.Fatal(err)
		}
	}

	rollback := errors.New("rollback job")
	err := sqlite.NewTxRepository(h.db).WithTransaction(ctx, func(ctx context.Context) error {
		if _, err := queue.Enqueue(ctx, kind, nil); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}

	mustEnqueue(kind, jobs.WithPriority(1))
	mustEnqueue(kind, jobs.WithPriority(9), jobs.WithDedupKey("same"))
	mustEnqueue(kind, jobs.WithPriority(10), jobs.WithRunAfter(time.Now().Add(time.Hour)))
	mustEnqueue(domain.JobEnrichTrack, jobs.WithPriority(99), jobs.WithDedupKey("same"))

	job, err := repo.Claim(ctx, []domain.JobType{kind}, time.Minute)
	if err != nil || job == nil || job.ID != 2 || job.Attempts != 1 {
		t.Fatalf("highest eligible job: %+v, error: %v", job, err)
	}
	if ok, err := repo.Finish(ctx, *job, "try again", time.Hour); err != nil || !ok {
		t.Fatalf("save retry: %v, %v", ok, err)
	}
	mustEnqueue(kind, jobs.WithPriority(100), jobs.WithDedupKey("same"))
	job, err = repo.Claim(ctx, []domain.JobType{kind}, time.Minute)
	if err != nil || job == nil || job.ID != 1 {
		t.Fatalf("next eligible job: %+v, error: %v", job, err)
	}
	if ok, err := repo.Finish(ctx, *job, "", 0); err != nil || !ok {
		t.Fatalf("save success: %v, %v", ok, err)
	}
	if job, err := repo.Claim(ctx, []domain.JobType{kind}, time.Minute); err != nil || job != nil {
		t.Fatalf("queue should be empty for type %s: %+v, %v", kind, job, err)
	}
}

// TestJobLeaseRecovery проверяет конкуренцию за одну задачу, восстановление
// после сбоя, смену токена и отказ в завершении задачи прежнему воркеру.
func TestJobLeaseRecovery(t *testing.T) {
	h := newRig(t)
	ctx := t.Context()
	otherDB, err := sqlite.Open(filepath.Join(h.root, "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherDB.Close() })
	first := sqlite.NewJobRepository(h.db)
	second := sqlite.NewJobRepository(otherDB)
	kind := domain.JobCollectBlobs
	types := []domain.JobType{kind}
	if _, err := jobs.New(first).Enqueue(ctx, kind, nil, jobs.WithMaxAttempts(2)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	claims := make(chan *domain.Job, 2)
	for _, repo := range []*sqlite.JobRepository{first, second} {
		wg.Go(func() {
			job, err := repo.Claim(ctx, types, time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if job != nil {
				claims <- job
			}
		})
	}
	wg.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatalf("claims=%d, want 1", len(claims))
	}
	old := <-claims
	if ok, err := first.Renew(ctx, *old, time.Minute); err != nil || !ok {
		t.Fatalf("renew current lease: %v, %v", ok, err)
	}

	expire := func(id domain.ID) {
		t.Helper()
		if _, err := h.db.ExecContext(ctx, "UPDATE jobs SET lease_until = 0 WHERE id = ?", id); err != nil {
			t.Fatal(err)
		}
	}
	expire(old.ID)
	current, err := second.Claim(ctx, types, time.Minute)
	if err != nil || current == nil || current.ID != old.ID || current.Attempts != 2 ||
		current.LeaseToken == old.LeaseToken {
		t.Fatalf("reclaim: %+v, %v", current, err)
	}
	if ok, err := first.Finish(ctx, *old, "", 0); err != nil || ok {
		t.Fatalf("old lease changed the job: %v, %v", ok, err)
	}
	expire(current.ID)
	if job, err := second.Claim(ctx, types, time.Minute); err != nil || job != nil {
		t.Fatalf("exhausted job was claimed: %+v, %v", job, err)
	}
	var status string
	if err := h.db.GetContext(ctx,
		&status, "SELECT status FROM jobs WHERE id = ?", current.ID); err != nil || status != "dead" {
		t.Fatalf("exhausted status=%q, error=%v", status, err)
	}
}

// TestPeriodicGCThroughWorker запускает реальный GC через SQLite-очередь,
// проверяет освобождение места и сохранение ссылочного блоба с нулевым счётчиком.
func TestPeriodicGCThroughWorker(t *testing.T) {
	h := newRig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	orphan := h.blob([]byte("orphan bytes"), domain.BlobAudio, "audio/wav")
	cover := h.blob([]byte("cover bytes"), domain.BlobImage, "image/png")
	if _, err := h.db.ExecContext(ctx,
		"INSERT INTO artists(public_id, name, norm_name, image_blob_id) VALUES (?, 'artist', 'artist', ?)",
		domain.NewPublicID(), cover.ID); err != nil {
		t.Fatal(err)
	}
	lost, err := h.store.Write(bytes.NewReader([]byte("unregistered bytes")))
	if err != nil {
		t.Fatal(err)
	}

	repo := sqlite.NewJobRepository(h.db)
	schedule := jobs.New(repo)
	kind := domain.JobCollectBlobs
	for range 2 {
		if err := schedule.EnsurePeriodic(ctx, kind, "storage", time.Hour, struct{}{}); err != nil {
			t.Fatal(err)
		}
	}
	collector := storage.NewCollector(h.store, h.blobs, h.log)
	completed := make(chan error, 1)
	worker, err := jobs.NewWorker(repo, h.log, jobs.WorkerConfig{
		Parallelism: 2, PollInterval: 5 * time.Millisecond, Lease: 15 * time.Second,
	}, map[domain.JobType]jobs.Registration{
		kind: {
			Timeout: 10 * time.Second,
			Handle: func(ctx context.Context, _ json.RawMessage) error {
				err := collector.Collect(ctx, 0)
				select {
				case completed <- err:
				case <-ctx.Done():
				}
				return err
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not complete GC")
	}

	assertMissing := func(path string) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(h.root, "blobs", path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	assertMissing(orphan.StoragePath)
	assertMissing(lost.StoragePath)
	if _, err := h.blobs.GetByID(ctx, orphan.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("orphan row still exists: %v", err)
	}
	if _, err := h.blobs.GetByID(ctx, cover.ID); err != nil {
		t.Fatalf("cover reference was lost: %v", err)
	}

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var row struct {
			Status   string
			Attempts int
			RunAfter int64 `db:"run_after"`
		}
		if err := h.db.GetContext(ctx, &row, "SELECT status, attempts, run_after FROM jobs"); err != nil {
			t.Fatal(err)
		}
		if row.Status == "pending" && row.Attempts == 0 && row.RunAfter > time.Now().UnixMilli() {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("periodic job was not rescheduled")
		}
	}
	var count int
	if err := h.db.GetContext(ctx, &count, "SELECT count(*) FROM jobs"); err != nil || count != 1 {
		t.Fatalf("periodic registrations=%d, want 1: %v", count, err)
	}
}
