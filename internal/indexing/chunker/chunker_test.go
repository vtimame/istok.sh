package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"s26.dev/istok-cli/internal/retrieval"
)

func TestChunksFallbackDeterministicBasic(t *testing.T) {
	chunks, err := Chunks("src/main.go", "go", []byte(""), "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].LineStart != 1 || chunks[0].LineEnd != 1 {
		t.Fatalf("invalid range %d:%d", chunks[0].LineStart, chunks[0].LineEnd)
	}
	if chunks[0].Provenance != "fallback" {
		t.Fatalf("expected fallback provenance, got %q", chunks[0].Provenance)
	}
}

func TestChunksCRLFNormalization(t *testing.T) {
	content := []byte("one\r\ntwo\r\nthree")
	chunks, err := Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].LineStart != 1 || chunks[0].LineEnd != 3 {
		t.Fatalf("expected lines 1-3, got %d-%d", chunks[0].LineStart, chunks[0].LineEnd)
	}
	if strings.Contains(chunks[0].Content, "\r") {
		t.Fatalf("chunk content contains CR: %q", chunks[0].Content)
	}
}

func TestChunksLineLimitsAndOverlap(t *testing.T) {
	lines := make([]string, 81)
	for i := 0; i < 81; i++ {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	content := []byte(strings.Join(lines, "\n"))

	chunks, err := New(Options{MaxLines: 80, MaxBytes: 1024, LineOverlap: 10}).Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].LineStart != 1 || chunks[0].LineEnd != 80 {
		t.Fatalf("unexpected first chunk range %d-%d", chunks[0].LineStart, chunks[0].LineEnd)
	}
	if chunks[1].LineStart != 71 || chunks[1].LineEnd != 81 {
		t.Fatalf("unexpected second chunk range %d-%d", chunks[1].LineStart, chunks[1].LineEnd)
	}
	for _, chunk := range chunks {
		if chunk.LineEnd-chunk.LineStart+1 > 80 {
			t.Fatalf("chunk exceeds max lines: %d", chunk.LineEnd-chunk.LineStart+1)
		}
	}
}

func TestChunksByteLimitsWithOverlapAndLongLine(t *testing.T) {
	line := strings.Repeat("a", 20)
	lines := []string{}
	for i := 0; i < 10; i++ {
		lines = append(lines, line)
	}
	lines = append(lines, strings.Repeat("b", 25))
	content := []byte(strings.Join(lines, "\n"))

	chunks, err := New(Options{MaxLines: 3, MaxBytes: 32, LineOverlap: 2}).Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk.Content) > 32 {
			t.Fatalf("chunk content too large: %d", len(chunk.Content))
		}
		if utf8.ValidString(chunk.Content) == false {
			t.Fatalf("chunk content invalid utf8")
		}
	}
}

func TestChunksLongUTF8LineDisambiguation(t *testing.T) {
	content := []byte(strings.Repeat("😀", 3000))
	chunks, err := Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) <= 1 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	ids := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.Content) > len(content) {
			t.Fatalf("invalid chunk length")
		}
		if !utf8.ValidString(chunk.Content) {
			t.Fatalf("chunk content must be valid utf8")
		}
		if _, ok := ids[chunk.ID]; ok {
			t.Fatalf("duplicate chunk id %q", chunk.ID)
		}
		ids[chunk.ID] = struct{}{}
	}
}

func TestChunksLongLineDeterministicIDs(t *testing.T) {
	content := []byte(strings.Repeat("😀", 3000))
	chunksA, err := New(Options{MaxBytes: 16}).Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	chunksB, err := New(Options{MaxBytes: 16}).Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunksA) != len(chunksB) {
		t.Fatalf("chunk count mismatch: %d != %d", len(chunksA), len(chunksB))
	}
	for i := range chunksA {
		if chunksA[i].ID != chunksB[i].ID {
			t.Fatalf("non-deterministic id at %d: %q != %q", i, chunksA[i].ID, chunksB[i].ID)
		}
		if chunksA[i].ID == chunksA[0].ID && i > 0 {
			t.Fatalf("duplicate IDs across chunks")
		}
	}
}

func TestChunksDiscriminatorAndDeterministicFunction(t *testing.T) {
	content := []byte(strings.Repeat("α", 3000))
	h := sha256.Sum256([]byte("x"))
	hash := hex.EncodeToString(h[:])
	chunks, err := New(Options{MaxBytes: 128}).Chunks("a.go", "go", content, hash, nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected long line split, got %d", len(chunks))
	}
	for i := range chunks {
		did := retrieval.DeterministicChunkID("a.go", hash, 1, 1, chunks[i].ChunkDiscriminator)
		if did != chunks[i].ID {
			t.Fatalf("chunk id mismatch at %d: %q != %q", i, did, chunks[i].ID)
		}
	}
}

func TestChunksLongLineBetweenRegularLinesIsNotDuplicated(t *testing.T) {
	longLine := strings.Repeat("refund", 2000)
	content := []byte("before\n" + longLine + "\nafter")

	chunks, err := New(Options{MaxBytes: 128, LineOverlap: 10}).Chunks("a.go", "go", content, "h1", nil)
	if err != nil {
		t.Fatalf("Chunks() error = %v", err)
	}

	var reconstructed strings.Builder
	longLineChunks := 0
	for _, chunk := range chunks {
		if chunk.LineStart == 2 && chunk.LineEnd == 2 {
			longLineChunks++
			reconstructed.WriteString(chunk.Content)
		}
	}

	if reconstructed.String() != longLine {
		t.Fatalf("long line reconstruction differs: got %d bytes, want %d", reconstructed.Len(), len(longLine))
	}
	if want := (len(longLine) + 127) / 128; longLineChunks != want {
		t.Fatalf("long line chunk count = %d, want %d", longLineChunks, want)
	}
}

func TestChunksRejectInvalidUTF8(t *testing.T) {
	_, err := Chunks("a.go", "go", []byte{0xff}, "h1", nil)
	if err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
}
