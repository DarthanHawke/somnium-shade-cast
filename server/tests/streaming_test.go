package test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
)

// TestStreamingDeniesAccessToAnotherUsersTrack проверяет, что stream и download отвечают
// другому пользователю 404 с запретом кэширования.
func TestStreamingDeniesAccessToAnotherUsersTrack(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	other := h.user("other")

	result, err := h.fixedService(nil, nil).Upload(
		context.Background(),
		ingest.UploadInput{OwnerID: owner.ID},
		bytes.NewReader([]byte("private track payload")),
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, pattern := range []string{"/stream/%s", "/tracks/%s/download"} {
		path := fmt.Sprintf(pattern, result.Track.PublicID)
		t.Logf("checking %s", path)

		response := h.request(http.MethodGet, path, other.PublicID, nil, nil)
		responseBody(t, response, http.StatusNotFound)

		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

// TestTrackCoverAccessAndCaching проверяет доступ к обложке ghost-трека, выдачу байтового
// диапазона и проверку ETag.
// Отдельно проверяет отказ для другого пользователя, ответ 404 на старом маршруте и 410
// после исчезновения файла.
func TestTrackCoverAccessAndCaching(t *testing.T) {
	h := newRig(t)
	ctx := context.Background()
	owner := h.user("cover owner")
	other := h.user("cover other")
	track := h.ghost(owner, true)
	path := "/lib/tracks/" + string(track.PublicID) + "/cover"

	payload := []byte("image payload")
	blob := h.blob(payload, domain.BlobImage, "image/png")

	request := func(
		user domain.PublicID,
		headers map[string]string,
		status int,
		cache string,
	) (*http.Response, []byte) {
		t.Helper()
		res := h.request(http.MethodGet, path, user, nil, headers)
		body := responseBody(t, res, status)
		if got := res.Header.Get("Cache-Control"); got != cache {
			t.Fatalf("Cache-Control = %q, want %q", got, cache)
		}
		return res, body
	}

	request(owner.PublicID, nil, http.StatusNotFound, "no-store")

	album, err := h.albums.GetByID(ctx, *track.AlbumID)
	if err != nil {
		t.Fatal(err)
	}
	album.CoverBlobID = &blob.ID
	if err := h.albums.Update(ctx, album); err != nil {
		t.Fatal(err)
	}

	res, body := request(owner.PublicID, nil, http.StatusOK, "private, no-cache")
	if !bytes.Equal(body, payload) || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected cover: %q, %q", res.Header.Get("Content-Type"), body)
	}
	etag := res.Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	_, body = request(
		owner.PublicID,
		map[string]string{"Range": "bytes=0-3"},
		http.StatusPartialContent,
		"private, no-cache",
	)
	if !bytes.Equal(body, payload[:4]) {
		t.Fatalf("unexpected range: %q", body)
	}
	request(
		owner.PublicID,
		map[string]string{"Range": "bytes=500-"},
		http.StatusRequestedRangeNotSatisfiable,
		"no-store",
	)

	conditional := map[string]string{"If-None-Match": etag}
	_, body = request(owner.PublicID, conditional, http.StatusNotModified, "private, no-cache")
	if len(body) != 0 {
		t.Fatalf("304 has a body: %q", body)
	}
	request(other.PublicID, conditional, http.StatusNotFound, "no-store")

	oldURL := fmt.Sprintf("/images/%d", blob.ID)
	responseBody(t, h.request(http.MethodGet, oldURL, owner.PublicID, nil, nil), http.StatusNotFound)

	if err := h.store.Remove(blob.StoragePath); err != nil {
		t.Fatal(err)
	}
	request(owner.PublicID, nil, http.StatusGone, "no-store")
	request(owner.PublicID, conditional, http.StatusGone, "no-store")
}

// TestStreamingRejectsNonAudioBlob проверяет, что stream и download отвечают 404, если
// вместо аудио к треку привязан блоб изображения.
func TestStreamingRejectsNonAudioBlob(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	track := h.ghost(owner, false)
	blob := h.blob([]byte("image payload"), domain.BlobImage, "image/png")
	track.Status = domain.TrackUploaded
	track.BlobID = &blob.ID
	if err := h.tracks.Materialize(context.Background(), track); err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"/stream/%s", "/tracks/%s/download"} {
		path := fmt.Sprintf(pattern, track.PublicID)
		responseBody(t, h.request(http.MethodGet, path, owner.PublicID, nil, nil), http.StatusNotFound)
	}
}
