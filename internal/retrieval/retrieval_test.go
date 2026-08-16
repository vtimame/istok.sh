package retrieval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeAndExpandIdentifiersCases(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "create_refund",
			input:    "create_refund",
			expected: []string{"create_refund", "create", "refund"},
		},
		{
			name:     "createRefund",
			input:    "CreateRefund",
			expected: []string{"CreateRefund", "createrefund", "create", "refund"},
		},
		{
			name:     "httpServer",
			input:    "HTTPServer",
			expected: []string{"HTTPServer", "httpserver", "http", "server"},
		},
		{
			name:     "unicode_dupes",
			input:    "Проверка_Проверка",
			expected: []string{"Проверка_Проверка", "проверка_проверка", "проверка"},
		},
		{
			name:     "unicode_acronym",
			input:    "HTTP_ответ",
			expected: []string{"HTTP_ответ", "http_ответ", "http", "ответ"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeAndExpandIdentifiers(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("len mismatch: %d != %d: %#v", len(got), len(tc.expected), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("token %d: got %q, want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestChunkJSONExcludesIndexStorageHints(t *testing.T) {
	value, err := json.Marshal(Chunk{
		ID:                 "chunk",
		SymbolNormalized:   "createservice",
		ChunkDiscriminator: "line/1/segment/2",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	encoded := string(value)
	for _, internalField := range []string{"symbol_normalized", "chunk_discriminator"} {
		if strings.Contains(encoded, internalField) {
			t.Fatalf("storage-only field %q leaked into retrieval JSON: %s", internalField, encoded)
		}
	}
}

func TestDeterministicChunkIDIgnoresEmptyDiscriminator(t *testing.T) {
	without := DeterministicChunkID("internal/a.go", "hash", 1, 4)
	withEmpty := DeterministicChunkID("internal/a.go", "hash", 1, 4, "")

	if without != withEmpty {
		t.Fatalf("empty discriminator changed chunk ID: %q != %q", without, withEmpty)
	}
}

func TestNormalizePathProducesProjectRelativeCanonicalPath(t *testing.T) {
	tests := map[string]string{
		"./internal/../internal/order/service.go": "internal/order/service.go",
		`internal\order\service.go`:               "internal/order/service.go",
		"../outside.go":                           "",
		"/absolute.go":                            "",
		`C:\absolute.go`:                          "",
	}

	for input, expected := range tests {
		if got := NormalizePath(input); got != expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", input, got, expected)
		}
	}
}
