package test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/respond"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

// TestSQLiteReplacementPreservesForeignKeys получает два сменившихся соединения из пула.
func TestSQLiteReplacementPreservesForeignKeys(t *testing.T) {
	h := newRig(t)
	h.db.SetMaxIdleConns(0)
	for range 2 {
		var enabled int
		if err := h.db.Get(&enabled, "PRAGMA foreign_keys"); err != nil || enabled != 1 {
			t.Fatalf("foreign_keys=%d: %v", enabled, err)
		}
	}
}

// TestGhostConstraintAndLegacyBlobPath проверяет правило статуса и старый ключ Windows.
func TestGhostConstraintAndLegacyBlobPath(t *testing.T) {
	h := newRig(t)
	ctx := t.Context()
	owner := h.user("owner")
	ghost := h.ghost(owner, false)
	data := []byte("encrypted fixture")
	blob := h.blob(data, domain.BlobAudio, "audio/wav")
	if _, err := h.db.Exec("UPDATE tracks SET blob_id = ? WHERE id = ?", blob.ID, ghost.ID); err == nil {
		t.Fatal("ghost accepted audio blob")
	}
	if _, err := h.db.Exec("UPDATE tracks SET status = 'uploaded' WHERE id = ?", ghost.ID); err == nil {
		t.Fatal("uploaded accepted null blob")
	}

	canonical := blob.StoragePath
	if strings.Count(canonical, "/") != 2 || strings.Contains(canonical, `\`) {
		t.Fatalf("noncanonical blob path: %q", canonical)
	}
	blob.StoragePath = strings.ReplaceAll(canonical, "/", `\`)
	if _, err := h.db.Exec("UPDATE blobs SET storage_path = ? WHERE id = ?", blob.StoragePath, blob.ID); err != nil {
		t.Fatal(err)
	}
	if removed, err := h.store.GC(ctx, h.blobs, 0); err != nil || removed != 0 {
		t.Fatalf("GC removed legacy key, removed=%d: %v", removed, err)
	}
	reader, err := h.store.Open(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if got, err := io.ReadAll(reader); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read legacy blob: %q, %v", got, err)
	}
}

// TestFTSReindexesCatalogUpdates проверяет новые и старые слова через рабочий HTTP-поиск.
func TestFTSReindexesCatalogUpdates(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	ctx := t.Context()
	svc := h.fixedService(nil, nil)
	item, err := svc.Accept(ctx, ingest.UploadInput{OwnerID: owner.ID}, "rename-fixture", strings.NewReader("audio"))
	if err != nil {
		t.Fatal(err)
	}
	stop := h.startUploadWorker(svc.Process)
	defer stop()
	result := h.waitUploadItem(ctx, item)
	if result.TrackPublicID == nil {
		t.Fatalf("no track: %+v", result)
	}
	track, err := h.tracks.GetByPublicID(ctx, *result.TrackPublicID)
	if err != nil {
		t.Fatal(err)
	}
	assertSearch := func(query string, count int) {
		t.Helper()
		found := decode[[]dto.TrackResponse](t,
			responseBody(t, h.request("GET", "/lib/tracks?q="+query, owner.PublicID, nil, nil), 200))
		if len(found) != count {
			t.Fatalf("search %q returned %d, want %d", query, len(found), count)
		}
	}
	assertSearch("artist", 1)
	assertSearch("album", 1)
	artist, err := h.artists.GetByID(ctx, track.ArtistID)
	if err != nil {
		t.Fatal(err)
	}
	artist.Name = "Renamed Performer"
	if err := h.artists.Update(ctx, artist); err != nil {
		t.Fatal(err)
	}
	album, err := h.albums.GetByID(ctx, *track.AlbumID)
	if err != nil {
		t.Fatal(err)
	}
	album.Title = "Renamed Release"
	if err := h.albums.Update(ctx, album); err != nil {
		t.Fatal(err)
	}
	assertSearch("artist", 0)
	assertSearch("album", 0)
	assertSearch("performer+release", 1)
}

// TestPruneFinishedJobs проверяет границу срока и размер пачки.
func TestPruneFinishedJobs(t *testing.T) {
	h := newRig(t)
	ctx := t.Context()
	cutoff := time.Unix(1_800_000_000, 0)
	for _, state := range []struct {
		status   string
		finished int64
	}{
		{"done", cutoff.UnixMilli() - 1},
		{"dead", cutoff.UnixMilli() - 1},
		{"done", cutoff.UnixMilli()},
		{"pending", cutoff.UnixMilli() - 1},
		{"failed", cutoff.UnixMilli() - 1},
	} {
		if _, err := h.db.Exec("INSERT INTO jobs(type, status, finished_at) VALUES ('test', ?, ?)", state.status, state.finished); err != nil {
			t.Fatal(err)
		}
	}
	repo := sqlite.NewJobRepository(h.db)
	for _, want := range []int64{1, 1, 0} {
		count, err := repo.PruneFinished(ctx, cutoff, 1)
		if err != nil || count != want {
			t.Fatalf("pruned %d, want %d: %v", count, want, err)
		}
	}
	if got := h.count("jobs"); got != 3 {
		t.Fatalf("%d jobs left, want 3", got)
	}
}

// coverMetadata подставляет встроенную обложку, сохраняя обычное разрешение каталога.
type coverMetadata struct {
	fixedMetadata
	cover []byte
}

func (m coverMetadata) Extract(ctx context.Context, src io.ReaderAt, size int64, name string) (*domain.ExtractedMetadata, error) {
	meta, err := m.fixedMetadata.Extract(ctx, src, size, name)
	if err != nil {
		return nil, err
	}
	meta.CoverData = m.cover
	return meta, nil
}

// TestUploadedCoverSurvivesCollection проверяет публикацию и очистку изображений.
func TestUploadedCoverSurvivesCollection(t *testing.T) {
	h := newRig(t)
	ctx := t.Context()
	owner := h.user("owner")
	makeCover := func(red uint8) []byte {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{R: red, A: 255})
		var out bytes.Buffer
		if err := png.Encode(&out, img); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	firstCover := makeCover(20)
	importTrack := func(key string, cover []byte) string {
		t.Helper()
		svc := h.service(coverMetadata{fixedMetadata{h.meta}, cover}, nil, nil)
		item, err := svc.Accept(ctx, ingest.UploadInput{OwnerID: owner.ID}, key, strings.NewReader(key))
		if err != nil {
			t.Fatal(err)
		}
		stop := h.startUploadWorker(svc.Process)
		defer stop()
		result := h.waitUploadItem(ctx, item)
		if result.TrackPublicID == nil {
			t.Fatalf("upload failed: %+v", result)
		}
		return string(*result.TrackPublicID)
	}
	first := importTrack("first-cover", firstCover)
	second := importTrack("second-cover", makeCover(90))

	if err := storage.NewCollector(h.store, h.blobs, h.log).Collect(ctx, 0); err != nil {
		t.Fatal(err)
	}
	for _, publicID := range []string{first, second} {
		path := "/lib/tracks/" + publicID + "/cover"
		res := h.request("GET", path, owner.PublicID, nil, nil)
		if got := responseBody(t, res, 200); !bytes.Equal(got, firstCover) || res.Header.Get("Content-Type") != "image/png" {
			t.Fatal("wrong album cover")
		}
	}
	if got := h.count("blobs"); got != 3 {
		t.Fatalf("%d blobs left, want two tracks and their shared cover", got)
	}
}

// TestPaginationContract проверяет ошибки параметров двух списков.
func TestPaginationContract(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	for _, path := range []string{"/lib/tracks", "/ingest/uploads"} {
		for param, field := range map[string]string{"limit=": "limit", "limit=201": "limit", "limit=2&limit=3": "limit", "offset=-1": "offset"} {
			got := decode[respond.ErrorBody](t,
				responseBody(t, h.request("GET", path+"?"+param, owner.PublicID, nil, nil), http.StatusBadRequest))
			if got.Field != field {
				t.Fatalf("%s?%s returned %+v", path, param, got)
			}
		}
	}
}
