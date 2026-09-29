package storage_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
	"github.com/DarthanHawke/somnium-shade-cast/server/internal/storage"
)

func testStore(t *testing.T) *storage.BlobStore {
	t.Helper()
	mk := make([]byte, 32)
	rand.Read(mk)
	km, err := storage.NewKeyManager("test-key-1", mk)
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewBlobStore(t.TempDir(), km)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeBlob(t *testing.T, s *storage.BlobStore, data []byte) *domain.Blob {
	t.Helper()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	res, err := s.Write(sha, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.NewBlob(sha, int64(len(data)), res.SizeEncrypted,
		"audio/mpeg", domain.BlobAudio, res.WrappedKey, res.MasterKeyID, res.ChunkSize)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundtrip(t *testing.T) {
	s := testStore(t)
	// размер специально НЕ кратен чанку и больше нескольких чанков
	data := make([]byte, 3*domain.DefaultChunkSize+12345)
	rand.Read(data)
	b := writeBlob(t, s, data)

	rc, err := s.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, data) {
		t.Fatal("full read mismatch")
	}
}

func TestRangeReads(t *testing.T) {
	s := testStore(t)
	data := make([]byte, 3*domain.DefaultChunkSize+999)
	rand.Read(data)
	b := writeBlob(t, s, data)

	cases := []struct{ off, ln int64 }{
		{0, 10},                           // начало
		{int64(len(data)) - 10, 10},       // хвост
		{domain.DefaultChunkSize - 5, 10}, // через границу чанков
		{domain.DefaultChunkSize, domain.DefaultChunkSize}, // ровно чанк
		{7, -1},                   // открытый конец
		{int64(len(data)) - 1, 1}, // последний байт
	}
	for _, c := range cases {
		rc, rng, err := s.OpenRange(b, c.off, c.ln)
		if err != nil {
			t.Fatalf("off=%d ln=%d: %v", c.off, c.ln, err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		want := data[rng.Offset : rng.Offset+rng.Length]
		if !bytes.Equal(got, want) {
			t.Fatalf("off=%d ln=%d: mismatch (%d vs %d bytes)", c.off, c.ln, len(got), len(want))
		}
	}
}
