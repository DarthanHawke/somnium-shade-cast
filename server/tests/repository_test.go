package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/ingest"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/repository/sqlite"
)

// TestUserArtistAlbumRepositoryContracts проверяет изменение активности пользователя и
// основные операции SQLite с исполнителями и альбомами.
// Повторная вставка исполнителя должна приводить к ErrConflict, а удалённый пользователь
// больше не находится.
func TestUserArtistAlbumRepositoryContracts(t *testing.T) {
	h := newRig(t)
	ctx := context.Background()
	user := h.user("owner")
	for _, active := range []bool{false, true} {
		var err error
		if active {
			err = h.users.Activate(ctx, user.PublicID)
		} else {
			err = h.users.Deactivate(ctx, user.PublicID)
		}
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.users.GetByPublicID(ctx, user.PublicID)
		if err != nil || got.IsActive != active {
			t.Fatalf("active=%t got=%+v err=%v", active, got, err)
		}
	}
	artist, err := domain.NewArtist("The Artist")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.artists.Insert(ctx, artist); err != nil {
		t.Fatal(err)
	}
	for _, get := range []func() (*domain.Artist, error){
		func() (*domain.Artist, error) { return h.artists.GetByID(ctx, artist.ID) },
		func() (*domain.Artist, error) { return h.artists.GetByPublicID(ctx, artist.PublicID) },
		func() (*domain.Artist, error) { return h.artists.GetByNormName(ctx, artist.NormName) },
	} {
		got, err := get()
		if err != nil || got.PublicID != artist.PublicID {
			t.Fatalf("artist round trip failed: %v", err)
		}
	}
	duplicate, _ := domain.NewArtist("The Artist")
	if err := h.artists.Insert(ctx, duplicate); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("artist uniqueness lost: %v", err)
	}
	artist.Name = "Updated Artist"
	artist.NormName = domain.Normalize(artist.Name)
	if err := h.artists.Update(ctx, artist); err != nil {
		t.Fatal(err)
	}
	if got, err := h.artists.Search(ctx, "%Updated%", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("artist search: %v %v", got, err)
	}
	if got, err := h.artists.List(ctx, 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("artist list: %v %v", got, err)
	}
	year := 2026
	album, err := domain.NewAlbum(artist.ID, "Album", &year, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.albums.Insert(ctx, album); err != nil {
		t.Fatal(err)
	}
	if got, err := h.albums.GetByID(ctx, album.ID); err != nil || got.Year == nil || *got.Year != year {
		t.Fatalf("album round trip: %+v %v", got, err)
	}
	if got, err := h.albums.GetByPublicID(ctx, album.PublicID); err != nil || got.ArtistID != artist.ID {
		t.Fatalf("album public ID: %+v %v", got, err)
	}
	if _, err := h.albums.GetByArtistAndNormTitle(ctx, artist.ID, album.NormTitle); err != nil {
		t.Fatal(err)
	}
	album.Title = "Updated Album"
	album.NormTitle = domain.Normalize(album.Title)
	if err := h.albums.Update(ctx, album); err != nil {
		t.Fatal(err)
	}
	if got, err := h.albums.Search(ctx, "%Updated%", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("album search: %v %v", got, err)
	}
	if got, err := h.albums.List(ctx, 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("album list: %v %v", got, err)
	}
	if err := h.albums.Delete(ctx, album.PublicID); err != nil {
		t.Fatal(err)
	}
	if err := h.artists.Delete(ctx, artist.PublicID); err != nil {
		t.Fatal(err)
	}
	if err := h.users.Delete(ctx, user.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.users.GetByPublicID(ctx, user.PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted user found: %v", err)
	}
}

// TestTrackProjectionAndStablePagination проверяет устойчивый порядок страниц при
// одинаковом времени создания, поля, допускающие NULL и сохранение миллисекунд.
// Также проверяет обновление трека, пакетное получение имён и повторное удаление.
func TestTrackProjectionAndStablePagination(t *testing.T) {
	h := newRig(t)
	ctx := context.Background()
	owner := h.user("owner")
	artist, _ := domain.NewArtist("Artist")
	if err := h.artists.Insert(ctx, artist); err != nil {
		t.Fatal(err)
	}
	var tracks []*domain.Track
	for i := range 3 {

		track, err := domain.NewTrack(domain.NewTrackParams{
			OwnerID:          owner.ID,
			ArtistID:         artist.ID,
			AlbumID:          nil,
			Title:            fmt.Sprintf("Track %d", i),
			TrackNo:          nil,
			DiscNo:           0,
			DurationMS:       nil,
			Status:           domain.TrackGhost,
			BlobID:           nil,
			Fingerprint:      "",
			FPDuration:       0,
			IsIntentionalDup: false,
			OriginalFilename: "",
			Codec:            "",
			BitrateKbps:      nil,
			SampleRate:       nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		track.CreatedAt = domain.NewUnixTimeMillis(1700000000123)
		track.UpdatedAt = track.CreatedAt
		if err := h.tracks.Insert(ctx, track); err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, track)
	}
	library := sqlite.NewLibraryRepository(h.db)
	for offset := range 3 {
		views, err := library.ListTracksByOwner(ctx, owner.ID, offset, 1)
		if err != nil || len(views) != 1 || views[0].PublicID != tracks[2-offset].PublicID {
			t.Fatalf("library offset=%d: %+v %v", offset, views, err)
		}
		if views[0].AlbumPublicID != nil || views[0].DurationMS != nil ||
			views[0].CreatedAt.UnixMilli() != 1700000000123 {
			t.Fatal("nullable fields or milliseconds lost")
		}
		rows, err := h.tracks.ListByOwner(ctx, owner.ID, offset, 1)
		if err != nil || len(rows) != 1 || rows[0].PublicID != tracks[2-offset].PublicID {
			t.Errorf("track repository order unstable at offset=%d: %+v %v", offset, rows, err)
		}
	}
	duration := int64(123456)
	tracks[0].DurationMS = &duration
	tracks[0].Title = "Changed"
	if err := h.tracks.Update(ctx, tracks[0]); err != nil {
		t.Fatal(err)
	}
	got, err := h.tracks.GetByPublicID(ctx, tracks[0].PublicID)
	if err != nil || got.Title != "Changed" || got.DurationMS == nil || *got.DurationMS != duration {
		t.Fatalf("track update: %+v %v", got, err)
	}
	names, albums, err := h.tracks.ArtistAlbumNames(ctx, []domain.ID{artist.ID}, nil)
	if err != nil || names[artist.ID] != artist.Name || len(albums) != 0 {
		t.Fatalf("batch names: %v %v %v", names, albums, err)
	}
	if err := h.tracks.Delete(ctx, tracks[0].PublicID); err != nil {
		t.Fatal(err)
	}
	if err := h.tracks.Delete(ctx, tracks[0].PublicID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing track delete: %v", err)
	}
}

// TestTransactionCommitRollbackNestedAndPanic проверяет фиксацию, откат при ошибке, отказ
// от вложенной транзакции и откат при панике.
// После паники соединение должно оставаться пригодным для работы с включёнными внешними
// ключами.
func TestTransactionCommitRollbackNestedAndPanic(t *testing.T) {
	h := newRig(t)
	tx := sqlite.NewTxRepository(h.db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	insert := func(ctx context.Context, name string) error {
		u, err := domain.NewUser(name, "", domain.RoleMember)
		if err != nil {
			return err
		}
		return h.users.Insert(ctx, u)
	}
	if err := tx.WithTransaction(ctx, func(ctx context.Context) error {
		return insert(ctx, "committed")
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("rollback requested")
	err := tx.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := insert(txCtx, "rolled-back"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || h.count("users") != 1 {
		t.Fatalf("rollback failed: %v", err)
	}

	nestedCalled := false
	err = tx.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := insert(txCtx, "outer-rolled-back"); err != nil {
			return err
		}
		return tx.WithTransaction(txCtx, func(context.Context) error {
			nestedCalled = true
			return nil
		})
	})
	if err == nil {
		t.Fatal("nested transaction was accepted")
	}
	if nestedCalled {
		t.Fatal("nested callback was called")
	}
	if h.count("users") != 1 {
		t.Fatal("outer transaction did not roll back after nested rejection")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("transaction swallowed panic")
			}
		}()
		tx.WithTransaction(ctx, func(ctx context.Context) error {
			if err := insert(ctx, "panic"); err != nil {
				return err
			}
			panic("test")
		})
	}()
	if h.count("users") != 1 {
		t.Fatal("panic did not roll back")
	}
	if err := tx.WithTransaction(ctx, func(ctx context.Context) error {
		return insert(ctx, "after-panic")
	}); err != nil || h.count("users") != 2 {
		t.Fatalf("connection unusable after panic: %v", err)
	}
	var foreignKeys int
	if err := h.db.Get(&foreignKeys, "PRAGMA foreign_keys"); err != nil || foreignKeys != 1 {
		t.Fatal("foreign keys disabled")
	}
}

// blob записывает байты в зашифрованное хранилище и регистрирует метаданные блоба в SQLite.
func (h *rig) blob(data []byte, kind domain.BlobKind, mime string) *domain.Blob {
	h.t.Helper()
	wr, err := h.store.Write(bytes.NewReader(data))
	if err != nil {
		h.t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	b, err := domain.NewBlob(hex.EncodeToString(sum[:]), int64(len(data)),
		wr.SizeEncrypted, mime, kind, wr.WrappedKey, wr.MasterKeyID, wr.ChunkSize, wr.StoragePath)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.blobs.Insert(context.Background(), b); err != nil {
		h.t.Fatal(err)
	}
	return b
}

// TestBlobRepositoryLifecycle проверяет поиск блоба, уникальность SHA-256, подсчёт ссылок и
// удаление записи сироты с возвратом пути к файлу.
func TestBlobRepositoryLifecycle(t *testing.T) {
	h := newRig(t)
	ctx := context.Background()
	data := []byte("test image bytes")
	blob := h.blob(data, domain.BlobImage, "image/png")
	if _, err := h.blobs.GetByID(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}
	if b, err := h.blobs.GetBySHA256(ctx, blob.SHA256Plain); err != nil || b.ID != blob.ID {
		t.Fatalf("blob hash lookup: %+v %v", b, err)
	}
	if err := h.blobs.Insert(ctx, blob); !errors.Is(err, domain.ErrBlobAlreadyExist) {
		t.Fatalf("blob uniqueness lost: %v", err)
	}
	if yes, err := h.blobs.PathExists(ctx, blob.StoragePath); err != nil || !yes {
		t.Fatal("blob path lookup failed")
	}
	if list, err := h.blobs.ListOrphans(ctx, time.Now().Add(time.Second).UnixMilli()); err != nil ||
		len(list) != 1 {
		t.Fatalf("orphans: %v %v", list, err)
	}
	if err := h.blobs.IncRef(ctx, blob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.blobs.DeleteIfOrphan(ctx, blob.ID); !errors.Is(err, domain.ErrBlobInUse) {
		t.Fatalf("referenced blob deleted: %v", err)
	}
	if n, err := h.blobs.DecRef(ctx, blob.ID); err != nil || n != 0 {
		t.Fatalf("decrement: %d %v", n, err)
	}
	if _, err := h.blobs.DecRef(ctx, blob.ID); !errors.Is(err, domain.ErrAlreadyOrphan) {
		t.Fatalf("negative refcount allowed: %v", err)
	}
	path, err := h.blobs.DeleteIfOrphan(ctx, blob.ID)
	if err != nil || path != blob.StoragePath {
		t.Fatalf("orphan delete: %s %v", path, err)
	}
	if err := h.store.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// TestCorruptionAfterFirstChunkAbortsHTTPBody проверяет, что повреждение позднего чанка
// прерывает уже начатую HTTP-выдачу и клиент получает ошибку чтения неполного тела.
func TestCorruptionAfterFirstChunkAbortsHTTPBody(t *testing.T) {
	h := newRig(t)
	owner := h.user("owner")
	data := bytes.Repeat([]byte{42}, 2*domain.DefaultChunkSize+123)
	result, err := h.fixedService(nil, nil).Upload(context.Background(),
		ingest.UploadInput{OwnerID: owner.ID}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := h.blobs.GetByID(context.Background(), *result.Track.BlobID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(h.root, "blobs", blob.StoragePath), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	offset := blob.ChunkOffsetOnDisk(1) + domain.GCMNonceSize + 3
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, offset); err != nil {
		f.Close()
		t.Fatal(err)
	}
	b[0] ^= 255
	_, err = f.WriteAt(b, offset)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	response := h.request("GET", "/stream/"+string(result.Track.PublicID), owner.PublicID, nil, nil)
	defer response.Body.Close()
	got, err := io.ReadAll(response.Body)
	if response.StatusCode != 200 || err == nil ||
		!bytes.Equal(got, data[:len(got)]) ||
		len(got) >= len(data) {
		t.Fatalf("corrupt stream reported success: status=%d bytes=%d err=%v",
			response.StatusCode, len(got), err)
	}
}
