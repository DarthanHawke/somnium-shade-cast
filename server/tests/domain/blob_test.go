package domain_test

import (
	"testing"

	"github.com/DarthanHawke/somnium-shade-cast/server/internal/domain"
)

func blob(sizePlain int64, chunkSize int) *domain.Blob {
	return &domain.Blob{SizePlain: sizePlain, ChunkSize: chunkSize}
}

func TestNumChunks(t *testing.T) {
	tests := []struct {
		size int64
		cs   int
		want int64
	}{
		{100, 100, 1}, // ровно один чанк
		{101, 100, 2}, // чуть больше
		{99, 100, 1},  // меньше чанка
		{300, 100, 3}, // ровно три
		{1, 100, 1},
	}
	for _, tt := range tests {
		if got := blob(tt.size, tt.cs).NumChunks(); got != tt.want {
			t.Errorf("size=%d cs=%d: got %d, want %d", tt.size, tt.cs, got, tt.want)
		}
	}
}

func TestPlanAndRange(t *testing.T) {
	b := blob(1000, 100) // 10 чанков по 100

	r, err := b.ResolveRange(250, 300) // байты 250..549
	if err != nil {
		t.Fatal(err)
	}
	p := b.Plan(r)
	if p.FirstChunk != 2 || p.LastChunk != 5 || p.SkipInFirst != 50 || p.Length != 300 {
		t.Errorf("got %+v", p)
	}

	// открытый конец
	r, _ = b.ResolveRange(950, -1)
	if r.Length != 50 {
		t.Errorf("open-ended: got len %d, want 50", r.Length)
	}

	// за границей
	if _, err := b.ResolveRange(1000, 1); err == nil {
		t.Error("expected error for offset == size")
	}
}

func TestChunkOffsetOnDisk(t *testing.T) {
	b := blob(1000, 100)
	// чанк на диске = 100 + 28 (nonce+tag) = 128
	if got := b.ChunkOffsetOnDisk(3); got != 3*128 {
		t.Errorf("got %d, want %d", got, 3*128)
	}
}

func TestPlainChunkLen_LastChunk(t *testing.T) {
	if got := blob(250, 100).PlainChunkLen(2); got != 50 {
		t.Errorf("last chunk: got %d, want 50", got)
	}
	if got := blob(300, 100).PlainChunkLen(2); got != 100 {
		t.Errorf("exact last chunk: got %d, want 100", got)
	}
}
