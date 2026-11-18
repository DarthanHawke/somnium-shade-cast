package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/jobs"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

const uploadsPath = "/ingest/uploads"

// startUploadWorker запускает только задачи загрузки. Возвращённая функция
// отменяет работу и дожидается завершения воркера.
func (h *rig) startUploadWorker(handle jobs.Handler) func() {
	h.t.Helper()
	worker, err := jobs.NewWorker(sqlite.NewJobRepository(h.db), h.log, jobs.WorkerConfig{
		Parallelism: 2, PollInterval: 10 * time.Millisecond, Lease: time.Minute,
	}, map[domain.JobType]jobs.Registration{
		domain.JobUploadTrack: {Handle: handle, Timeout: 15 * time.Second},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(h.t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

// waitUntil ограничивает ожидание фонового события временем теста.
func (h *rig) waitUntil(label string, check func() bool) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.t.Context(), 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !check() {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			h.t.Fatalf("timed out waiting for %s: %v", label, ctx.Err())
		}
	}
}

// waitUpload ждёт конечного состояния через указанный способ чтения статуса.
func (h *rig) waitUpload(id domain.PublicID, read func() dto.TrackUploadResponse) dto.TrackUploadResponse {
	h.t.Helper()
	var item dto.TrackUploadResponse
	h.waitUntil("upload "+string(id), func() bool {
		item = read()
		if item.PublicID != id {
			h.t.Fatalf("upload public ID = %s, want %s", item.PublicID, id)
		}
		switch item.Status {
		case domain.UploadReady, domain.UploadFailed:
			if item.CompletedAt == nil {
				h.t.Fatalf("finished upload has no completion time: %+v", item)
			}
			return true
		case domain.UploadQueued, domain.UploadProcessing:
			return false
		default:
			h.t.Fatalf("unexpected upload status: %q", item.Status)
			return false
		}
	})
	return item
}

// getUploadHTTP читает публичное состояние загрузки через маршрут владельца.
func (h *rig) getUploadHTTP(owner, id domain.PublicID) dto.TrackUploadResponse {
	h.t.Helper()
	return decode[dto.TrackUploadResponse](h.t, responseBody(h.t,
		h.request(http.MethodGet, uploadsPath+"/"+string(id), owner, nil, nil), http.StatusOK))
}

// acceptUploadHTTP проверяет подтверждение приёма файла и ссылку на состояние.
func (h *rig) acceptUploadHTTP(
	owner domain.PublicID,
	key,
	meta string,
	data []byte,
	name string,
) dto.TrackUploadResponse {
	h.t.Helper()
	resp := h.multipartWithKey(owner, key, meta, data, name)
	item := decode[dto.TrackUploadResponse](h.t, responseBody(h.t, resp, http.StatusAccepted))
	if item.PublicID == "" || resp.Header.Get("Location") != uploadsPath+"/"+string(item.PublicID) {
		h.t.Fatalf("invalid acceptance: public_id=%q, Location=%q", item.PublicID, resp.Header.Get("Location"))
	}
	return item
}

// finishUploadHTTP проводит загрузку через HTTP и ждёт результата фоновой задачи.
func (h *rig) finishUploadHTTP(
	owner domain.PublicID,
	meta string,
	data []byte,
	name string,
) dto.TrackUploadResponse {
	h.t.Helper()
	accepted := h.acceptUploadHTTP(owner, string(domain.NewPublicID()), meta, data, name)
	ctx := h.t.Context()
	var jobID domain.ID
	if err := h.db.GetContext(
		ctx,
		&jobID,
		"SELECT job_id FROM track_uploads WHERE public_id = ?",
		accepted.PublicID,
	); err != nil || jobID == 0 {
		h.t.Fatalf("accepted upload job = %d, err=%v", jobID, err)
	}
	stop := h.startUploadWorker(h.upload.Process)
	defer stop()
	result := h.waitUpload(accepted.PublicID, func() dto.TrackUploadResponse {
		return h.getUploadHTTP(owner, accepted.PublicID)
	})
	h.waitJobStatus(ctx, jobID, "done")
	return result
}

// readyTrackHTTP получает представление трека после завершения загрузки.
func (h *rig) readyTrackHTTP(owner domain.PublicID, meta string, data []byte, name string) dto.TrackResponse {
	h.t.Helper()
	item := h.finishUploadHTTP(owner, meta, data, name)
	if item.Status != domain.UploadReady || item.TrackPublicID == nil || item.ErrorCode != nil {
		h.t.Fatalf("upload did not create a track: %+v", item)
	}
	return decode[dto.TrackResponse](h.t, responseBody(h.t, h.request(
		http.MethodGet, "/lib/tracks/"+string(*item.TrackPublicID), owner, nil, nil), http.StatusOK))
}

// waitUploadItem читает состояние через сервис, когда тест подменяет зависимости.
func (h *rig) waitUploadItem(ctx context.Context, item *domain.TrackUpload) dto.TrackUploadResponse {
	h.t.Helper()
	return h.waitUpload(item.PublicID, func() dto.TrackUploadResponse {
		current, err := h.upload.Get(ctx, item.OwnerID, item.PublicID)
		if err != nil {
			h.t.Fatal(err)
		}
		return dto.NewTrackUploadResponse(current)
	})
}

// processUpload принимает файл и ждёт результата с указанным обработчиком.
func (h *rig) processUpload(
	ctx context.Context,
	svc *ingest.Service,
	input ingest.UploadInput,
	data []byte,
) dto.TrackUploadResponse {
	h.t.Helper()
	item, err := svc.Accept(ctx, input, string(domain.NewPublicID()), bytes.NewReader(data))
	if err != nil {
		h.t.Fatal(err)
	}
	stop := h.startUploadWorker(svc.Process)
	defer stop()
	result := h.waitUploadItem(ctx, item)
	if item.JobID == nil {
		h.t.Fatal("accepted upload has no job")
	}
	h.waitJobStatus(ctx, *item.JobID, "done")
	return result
}

// uploadTrack создаёт готовый трек для сценариев, которым нужны данные библиотеки.
func (h *rig) uploadTrack(
	ctx context.Context,
	svc *ingest.Service,
	input ingest.UploadInput,
	data []byte,
) *domain.Track {
	h.t.Helper()
	item := h.processUpload(ctx, svc, input, data)
	if item.Status != domain.UploadReady || item.TrackPublicID == nil || item.ErrorCode != nil {
		h.t.Fatalf("upload did not create a track: %+v", item)
	}
	track, err := h.tracks.GetByPublicID(ctx, *item.TrackPublicID)
	if err != nil {
		h.t.Fatal(err)
	}
	return track
}

// waitFailedAttempt сверяет исходную ошибку обработчика и ждёт записи
// отложенного повтора в SQLite.
func (h *rig) waitFailedAttempt(ctx context.Context, svc *ingest.Service, item *domain.TrackUpload, want error) {
	h.t.Helper()
	if item.JobID == nil {
		h.t.Fatal("accepted upload has no job")
	}
	observed := make(chan error, 1)
	stop := h.startUploadWorker(func(ctx context.Context, payload json.RawMessage) error {
		err := svc.Process(ctx, payload)
		select {
		case observed <- err:
		default:
		}
		return err
	})
	defer stop()

	h.waitUntil("handler error", func() bool {
		select {
		case got := <-observed:
			if !errors.Is(got, want) {
				h.t.Fatalf("attempt err=%v, want %v", got, want)
			}
			return true
		default:
			return false
		}
	})
	h.waitJobStatus(ctx, *item.JobID, "failed")
}

// waitJobStatus ждёт, пока воркер зафиксирует состояние задачи в БД.
func (h *rig) waitJobStatus(ctx context.Context, jobID domain.ID, want string) {
	h.t.Helper()
	h.waitUntil("job status "+want, func() bool {
		var status string
		if err := h.db.GetContext(ctx, &status, "SELECT status FROM jobs WHERE id = ?", jobID); err != nil {
			h.t.Fatal(err)
		}
		return status == want
	})
}

// requireUploadFailure проверяет публичную причину отказа.
func requireUploadFailure(t *testing.T, item dto.TrackUploadResponse, code string) {
	t.Helper()
	if item.Status != domain.UploadFailed || item.TrackPublicID != nil ||
		item.ErrorCode == nil || *item.ErrorCode != code {
		t.Fatalf("upload = %+v, want failure %q", item, code)
	}
}

// TestAcceptedUploadSurvivesRestart проверяет повтор ключа, приватность,
// удержание файла до обработки и сохранение результата после очистки очереди.
func TestAcceptedUploadSurvivesRestart(t *testing.T) {
	h := newRig(t)
	owner, other := h.user("owner"), h.user("other")
	data := []byte("accepted audio")
	const key = "one-upload"
	first := h.acceptUploadHTTP(owner.PublicID, key, `{"force_dup":true}`, data, "track.mp3")
	if first.Status != domain.UploadQueued || first.CompletedAt != nil ||
		first.TrackPublicID != nil || first.ErrorCode != nil {
		t.Fatalf("unexpected initial state: %+v", first)
	}
	replay := h.acceptUploadHTTP(owner.PublicID, key, `{"force_dup":true}`, data, "track.mp3")
	if replay.PublicID != first.PublicID || h.count("track_uploads") != 1 || h.count("jobs") != 1 {
		t.Fatal("repeating the request created another upload or job")
	}
	responseBody(t, h.multipartWithKey(owner.PublicID, key, `{}`, data, "track.mp3"), http.StatusConflict)
	responseBody(t, h.request(http.MethodGet,
		uploadsPath+"/"+string(first.PublicID), other.PublicID, nil, nil), http.StatusNotFound)
	otherHistory := decode[[]dto.TrackUploadResponse](t, responseBody(t,
		h.request(http.MethodGet, uploadsPath, other.PublicID, nil, nil), http.StatusOK))
	if otherHistory == nil || len(otherHistory) != 0 {
		t.Fatalf("other user's history is not empty: %+v", otherHistory)
	}

	ctx := t.Context()
	var jobID domain.ID
	if err := h.db.GetContext(ctx, &jobID,
		"SELECT job_id FROM track_uploads WHERE public_id = ?", first.PublicID); err != nil || jobID == 0 {
		t.Fatalf("accepted upload job = %d, err=%v", jobID, err)
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE blobs SET created_at = 1"); err != nil {
		t.Fatal(err)
	}
	if err := storage.NewCollector(h.store, h.blobs, h.log).Collect(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if h.count("blobs") != 1 || h.count("tracks") != 0 {
		t.Fatal("GC changed the file held by a queued upload")
	}
	h.http.Close()
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	h.open()
	stop := h.startUploadWorker(h.fixedService(nil, nil).Process)
	defer stop()
	final := h.waitUpload(first.PublicID, func() dto.TrackUploadResponse {
		return h.getUploadHTTP(owner.PublicID, first.PublicID)
	})
	if final.Status != domain.UploadReady || final.TrackPublicID == nil {
		t.Fatalf("restart did not restore upload: %+v", final)
	}
	h.waitJobStatus(ctx, jobID, "done")
	stop()

	// Имитируем повторную выдачу задачи после коммита результата загрузки.
	if _, err := h.db.ExecContext(
		ctx,
		"UPDATE jobs SET status = 'pending', attempts = 0, run_after = 0 WHERE type = ?",
		domain.JobUploadTrack); err != nil {
		t.Fatal(err)
	}
	replayWorker := h.startUploadWorker(h.fixedService(nil, nil).Process)
	defer replayWorker()
	h.waitJobStatus(ctx, jobID, "done")
	replayWorker()

	if h.count("tracks") != 1 {
		t.Fatal("replayed job created a second track")
	}
	if _, err := h.db.ExecContext(ctx, "DELETE FROM jobs WHERE type = ?", domain.JobUploadTrack); err != nil {
		t.Fatal(err)
	}
	got := h.acceptUploadHTTP(owner.PublicID, key, `{"force_dup":true}`, data, "track.mp3")
	if got.PublicID != first.PublicID || got.Status != domain.UploadReady || got.TrackPublicID == nil ||
		*got.TrackPublicID != *final.TrackPublicID {
		t.Fatalf("job deletion broke idempotent result: %+v", got)
	}
	history := decode[[]dto.TrackUploadResponse](t, responseBody(t,
		h.request(http.MethodGet, uploadsPath, owner.PublicID, nil, nil), http.StatusOK))
	if len(history) != 1 || history[0].PublicID != first.PublicID {
		t.Fatalf("unexpected upload history: %+v", history)
	}
}

// TestExhaustedUploadReleasesFile проверяет, что исчерпание аренды сохраняет
// публичную причину отказа и освобождает файл для GC.
func TestExhaustedUploadReleasesFile(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	item := h.acceptUploadHTTP(owner.PublicID, "exhausted", `{}`, []byte("pending audio"), "pending.mp3")
	ctx := t.Context()
	if _, err := h.db.ExecContext(ctx,
		"UPDATE jobs SET max_attempts = 1 WHERE type = ?", domain.JobUploadTrack); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewJobRepository(h.db)
	types := []domain.JobType{domain.JobUploadTrack}
	job, err := repo.Claim(ctx, types, time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE jobs SET lease_until = 0 WHERE id = ?", job.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := repo.Claim(ctx, types, time.Minute); err != nil || next != nil {
		t.Fatalf("exhausted claim: %+v, %v", next, err)
	}
	got := h.getUploadHTTP(owner.PublicID, item.PublicID)
	requireUploadFailure(t, got, "processing_failed")
	if got.CompletedAt == nil {
		t.Fatal("exhausted upload has no completion time")
	}
	if _, err := h.db.ExecContext(ctx, "UPDATE blobs SET created_at = 1"); err != nil {
		t.Fatal(err)
	}
	if err := storage.NewCollector(h.store, h.blobs, h.log).Collect(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if h.count("blobs") != 0 || h.count("tracks") != 0 {
		t.Fatal("failed upload kept a file or created a track")
	}
}
