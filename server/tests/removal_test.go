package test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/api/dto"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
)

// TestLibraryRemovalPreservesSharedData проверяет, что удаление затрагивает
// только треки владельца и сохраняет общие записи каталога и блоб.
func TestLibraryRemovalPreservesSharedData(t *testing.T) {
	for _, scope := range []string{"tracks", "albums", "artists", "users"} {
		t.Run(scope, func(t *testing.T) {
			ctx := context.Background()
			h := newRig(t)
			other := h.user("other")
			owner := h.user("owner")
			svc := h.fixedService(nil, nil)
			audio := []byte("shared audio")
			first := h.uploadTrack(ctx, svc, ingest.UploadInput{OwnerID: owner.ID}, audio)
			second := h.uploadTrack(ctx, svc, ingest.UploadInput{OwnerID: other.ID}, audio)

			if scope != "tracks" {
				input := ingest.UploadInput{OwnerID: owner.ID, ForceDup: true}
				h.uploadTrack(ctx, svc, input, audio)
			}

			identity := owner.PublicID
			var path string
			switch scope {
			case "tracks":
				path = "/lib/tracks/" + string(first.PublicID)
				foreign := "/lib/tracks/" + string(second.PublicID)
				responseBody(t, h.request(http.MethodDelete, foreign, identity, nil, nil), http.StatusNotFound)
			case "albums":
				if first.AlbumID == nil {
					t.Fatal("test track has no album")
				}
				album, err := h.albums.GetByID(ctx, *first.AlbumID)
				if err != nil {
					t.Fatal(err)
				}
				path = "/lib/albums/" + string(album.PublicID)
			case "artists":
				artist, err := h.artists.GetByID(ctx, first.ArtistID)
				if err != nil {
					t.Fatal(err)
				}
				path = "/lib/artists/" + string(artist.PublicID)
			case "users":
				path = "/dev/users/" + string(owner.PublicID)
				identity = ""
			}
			if identity != "" {
				responseBody(t, h.request(http.MethodDelete, path, "", nil, nil), http.StatusUnauthorized)
			}
			responseBody(t, h.request(http.MethodDelete, path, identity, nil, nil), http.StatusNoContent)
			responseBody(t, h.request(http.MethodDelete, path, identity, nil, nil), http.StatusNotFound)
			if h.count("tracks") != 1 || h.count("albums") != 1 || h.count("artists") != 1 {
				t.Fatal("unexpected catalog after removal")
			}
			blob, err := h.blobs.GetByID(ctx, *second.BlobID)
			if err != nil || blob.RefCount != 1 {
				t.Fatalf("shared blob = %+v, err = %v", blob, err)
			}
			searchPath := "/lib/tracks?q=File+Tit"
			body := responseBody(t, h.request(http.MethodGet, searchPath, other.PublicID, nil, nil), http.StatusOK)
			tracks := decode[[]dto.TrackResponse](t, body)
			if len(tracks) != 1 || tracks[0].PublicID != string(second.PublicID) {
				t.Fatalf("HTTP search after removal returned %+v", tracks)
			}
			missing := h.request(http.MethodGet, "/lib/tracks?q=Unrelated", other.PublicID, nil, nil)
			if body := responseBody(t, missing, http.StatusOK); !bytes.Equal(bytes.TrimSpace(body), []byte("[]")) {
				t.Fatalf("nonmatching HTTP search returned %s", body)
			}

			stream := "/stream/" + string(second.PublicID)
			if body := responseBody(t, h.request(http.MethodGet, stream, other.PublicID, nil, nil), http.StatusOK); !bytes.Equal(body, audio) {
				t.Fatal("other user's audio changed")
			}
			if scope == "users" {
				if _, err := h.users.GetByPublicID(ctx, owner.PublicID); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("deleted profile lookup: %v", err)
				}
				next := h.user("new-owner")
				if next.ID <= owner.ID {
					t.Fatal("new user reused a deleted internal ID")
				}
			}
		})
	}
}

// TestDeletedTrackAndBlobIDsAreNotReused проверяет, что после физического
// удаления строки следующая загрузка получает новые ID трека и блоба.
func TestDeletedTrackAndBlobIDsAreNotReused(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	ctx := context.Background()
	svc := h.fixedService(nil, nil)
	input := ingest.UploadInput{OwnerID: owner.ID}

	first := h.uploadTrack(ctx, svc, input, []byte("first audio"))
	responseBody(t, h.request(http.MethodDelete,
		"/lib/tracks/"+string(first.PublicID), owner.PublicID, nil, nil), http.StatusNoContent)
	firstBlobID := *first.BlobID

	// Имитируем удаление метаданных блоба будущим GC в отсутствие других загрузок.
	if _, err := h.blobs.DeleteIfOrphan(ctx, firstBlobID); err != nil {
		t.Fatal(err)
	}

	second := h.uploadTrack(ctx, svc, input, []byte("second audio"))
	if second.ID <= first.ID || *second.BlobID <= firstBlobID {
		t.Fatalf("expected new IDs: track %d -> %d, blob %d -> %d",
			first.ID, second.ID, firstBlobID, *second.BlobID)
	}
}

// TestDeleteUserRollsBackLibrary проверяет откат очистки треков, индекса
// и счётчика блоба, если удалить сам профиль не удалось.
func TestDeleteUserRollsBackLibrary(t *testing.T) {
	h := newRig(t)
	ctx := context.Background()
	owner := h.user("owner")
	track := h.uploadTrack(ctx, h.fixedService(nil, nil),
		ingest.UploadInput{OwnerID: owner.ID}, []byte("audio"))
	_, err := h.db.ExecContext(ctx, `
		CREATE TRIGGER reject_user_delete BEFORE DELETE ON users
		BEGIN
			SELECT RAISE(ABORT, 'test deletion failure');
		END`)
	if err != nil {
		t.Fatal(err)
	}

	path := "/dev/users/" + string(owner.PublicID)
	responseBody(t, h.request(http.MethodDelete, path, "", nil, nil), http.StatusInternalServerError)

	if h.count("users") != 1 || h.count("tracks") != 1 {
		t.Fatal("failed deletion removed the user or track")
	}
	blob, err := h.blobs.GetByID(ctx, *track.BlobID)
	if err != nil || blob.RefCount != 1 {
		t.Fatalf("blob after rollback: %+v, err=%v", blob, err)
	}
}
