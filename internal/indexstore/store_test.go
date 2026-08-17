package indexstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/google/uuid"

	"s26.dev/istok-cli/internal/indexing/chunker"
	"s26.dev/istok-cli/internal/indexing/discovery"
	"s26.dev/istok-cli/internal/retrieval"
)

func TestCreateOpenMetadataAndMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lexical")
	projectID := uuid.NewString()
	epochID := uuid.NewString()

	store, err := Create(path, projectID, epochID, 0)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_ = store.Close()

	opened, err := Open(path, projectID, epochID, 0)
	if err != nil {
		t.Fatalf("Open(rev0) error = %v", err)
	}
	_ = opened.Close()

	if _, err := Open(path, projectID, epochID, 1); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
	if _, err := Open(path, projectID, "00000000-0000-0000-0000-000000000000", 0); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected incompatible epoch, got %v", err)
	}
	if _, err := Open(path, "00000000-0000-0000-0000-000000000000", epochID, 0); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected incompatible project, got %v", err)
	}
	if _, err := Open(filepath.Join(path, "missing"), projectID, epochID, 0); !errors.Is(err, ErrMissing) {
		t.Fatalf("expected missing, got %v", err)
	}

	indexHandle, err := bleve.Open(path)
	if err != nil {
		t.Fatalf("bleve.Open() error = %v", err)
	}
	_ = indexHandle.Close()
	if err := os.Remove(filepath.Join(path, "index_meta.json")); err != nil {
		t.Fatalf("os.Remove(index_meta.json) error = %v", err)
	}

	if _, err := Open(path, projectID, epochID, 0); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected open error for corruption, got %v", err)
	}
}

func TestCreateRejectsExistingPath(t *testing.T) {
	nonBleve := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(nonBleve, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Create(nonBleve, uuid.NewString(), uuid.NewString(), 1); err == nil {
		t.Fatalf("expected Create failure")
	}
}

func TestApplyIncrementalAndRevisionGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lexical")
	projectID := uuid.NewString()
	epochID := uuid.NewString()

	store, err := Create(path, projectID, epochID, 10)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer store.Close()

	firstA := []byte("package a\n\nfunc LegacyOnly() {}\n")
	newA := []byte("package a\n\nfunc Updated() {}\n")
	firstB := []byte("package b\n\nfunc B() {}\n")
	firstC := []byte("package c\n\nfunc C() {}\n")
	a, err := chunksFromPath("internal/a.go", "go", firstA)
	if err != nil {
		t.Fatalf("chunks internal/a.go error = %v", err)
	}
	b, err := chunksFromPath("internal/b.go", "go", firstB)
	if err != nil {
		t.Fatalf("chunks internal/b.go error = %v", err)
	}
	c, err := chunksFromPath("internal/c.go", "go", firstC)
	if err != nil {
		t.Fatalf("chunks internal/c.go error = %v", err)
	}

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: append(append(a, b...), c...)}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := store.Revision(); got != 11 {
		t.Fatalf("revision got %d, want 11", got)
	}

	beforeB, err := store.Search(retrieval.SearchRequest{Query: "internal/b.go"})
	if err != nil {
		t.Fatalf("Search(before) error = %v", err)
	}
	if len(beforeB) == 0 || beforeB[0].Path != "internal/b.go" {
		t.Fatalf("expected internal/b.go in before results: %#v", beforeB)
	}
	beforeAIDs := chunkIDsFromDocs(a)
	beforeBIDs := chunkIDsFromDocs(b)
	beforeCIDs := chunkIDsFromDocs(c)

	secondA, err := chunksFromPath("internal/a.go", "go", newA)
	if err != nil {
		t.Fatalf("chunks internal/a.go update error = %v", err)
	}
	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: secondA}); err != nil {
		t.Fatalf("Apply(update) error = %v", err)
	}

	if got := store.Revision(); got != 12 {
		t.Fatalf("revision got %d, want 12", got)
	}

	if legacy, err := store.Search(retrieval.SearchRequest{Query: "LegacyOnly"}); err != nil {
		t.Fatalf("Search(LegacyOnly) error = %v", err)
	} else if len(legacy) != 0 {
		t.Fatalf("expected old content to disappear after update, got %#v", legacy)
	}

	afterBIDs := chunkIDsFromSearchResult(store, t, "internal/b.go")
	if len(afterBIDs) != len(beforeBIDs) {
		t.Fatalf("unchanged path chunk count changed: %#v -> %#v", beforeBIDs, afterBIDs)
	}
	afterCIDs := chunkIDsFromSearchResult(store, t, "internal/c.go")
	if len(afterCIDs) != len(beforeCIDs) {
		t.Fatalf("unchanged path chunk count changed: %#v -> %#v", beforeCIDs, afterCIDs)
	}

	afterA := chunkIDsFromDocs(secondA)
	if equalStringSets(beforeAIDs, afterA) {
		t.Fatalf("expected updated path chunk IDs to change, got %#v", afterA)
	}

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, DeletePaths: []string{"internal/b.go"}}); err != nil {
		t.Fatalf("Apply(delete) error = %v", err)
	}

	if got := store.Revision(); got != 13 {
		t.Fatalf("revision got %d, want 13", got)
	}
	if got := chunkIDsFromSearchResult(store, t, "internal/b.go"); len(got) != 0 {
		t.Fatalf("expected delete-only update to remove internal/b.go, got %#v", got)
	}
	if got := chunkIDsFromSearchResult(store, t, "internal/c.go"); len(got) != len(beforeCIDs) {
		t.Fatalf("delete-only update changed unrelated path: %#v -> %#v", beforeCIDs, got)
	}

	err = store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, IndexRevision: 11, Add: secondA})
	if !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("expected revision mismatch, got %v", err)
	}
	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, IndexRevision: 14, Add: secondA}); err != nil {
		t.Fatalf("Apply with explicit revision error = %v", err)
	}
}

func TestApplyRejectsConflictingInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lexical")
	projectID := uuid.NewString()
	epochID := uuid.NewString()

	store, err := Create(path, projectID, epochID, 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer store.Close()

	docA1 := retrieval.Document{Path: "internal/conflict.go", LineStart: 1, LineEnd: 1, Content: "a", ContentHash: hashString("A"), Snippet: "a", Provenance: "fallback"}
	docA1.ID = retrieval.DeterministicChunkID(docA1.Path, docA1.ContentHash, docA1.LineStart, docA1.LineEnd)
	docA2 := retrieval.Document{Path: "internal/conflict.go", LineStart: 1, LineEnd: 1, Content: "a", ContentHash: hashString("B"), Snippet: "a", Provenance: "fallback"}
	docA2.ID = retrieval.DeterministicChunkID(docA2.Path, docA2.ContentHash, docA2.LineStart, docA2.LineEnd)

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: []retrieval.Document{docA1, docA2}}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected incompatible on conflicting content hash, got %v", err)
	}
}

func TestSearchRankingAndReasons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lexical")
	projectID := uuid.NewString()
	epochID := uuid.NewString()

	store, err := Create(path, projectID, epochID, 0)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer store.Close()

	docs := []retrieval.Document{
		{
			Path:               "internal/order/service.go",
			LineStart:          1,
			LineEnd:            1,
			Content:            "package order\n\nfunc serviceEntry() {}",
			ContentHash:        hashString("exact-symbol"),
			Snippet:            "func serviceEntry() {}",
			Provenance:         "fallback",
			Symbol:             "Helper",
			ChunkDiscriminator: "exact",
		},
		{
			Path:               "internal/order/identifier.go",
			LineStart:          1,
			LineEnd:            1,
			Content:            "package order\n\nfunc identifierOnly() {}",
			ContentHash:        hashString("identifier-only"),
			Snippet:            "func identifierOnly() {}",
			Provenance:         "fallback",
			Symbol:             "IdentifierOnly",
			Identifiers:        []string{"helper"},
			ChunkDiscriminator: "identifier",
		},
		{
			Path:               "internal/lexical/common.go",
			LineStart:          3,
			LineEnd:            3,
			Content:            "helper",
			ContentHash:        hashString("lexical-common"),
			Snippet:            "helper",
			Provenance:         "fallback",
			Symbol:             "LexicalA",
			ChunkDiscriminator: "a",
		},
		{
			Path:               "internal/lexical/common.go",
			LineStart:          3,
			LineEnd:            3,
			Content:            "helper",
			ContentHash:        hashString("lexical-common"),
			Snippet:            "helper",
			Provenance:         "fallback",
			Symbol:             "LexicalB",
			ChunkDiscriminator: "b",
		},
	}
	for index := range docs {
		docs[index].ID = retrieval.DeterministicChunkID(docs[index].Path, docs[index].ContentHash, docs[index].LineStart, docs[index].LineEnd)
	}
	for i := range docs {
		docs[i].ID = retrieval.DeterministicChunkID(docs[i].Path, docs[i].ContentHash, docs[i].LineStart, docs[i].LineEnd, docs[i].ChunkDiscriminator)
		docs[i].SymbolNormalized = strings.ToLower(docs[i].Symbol)
		if len(docs[i].Identifiers) == 0 && docs[i].Path != "internal/lexical/common.go" {
			docs[i].Identifiers = retrieval.NormalizeAndExpandIdentifiers(docs[i].Content)
		}
	}

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: docs}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	pathResults, err := store.Search(retrieval.SearchRequest{Query: "internal/order/service.go"})
	if err != nil {
		t.Fatalf("Search(path) error = %v", err)
	}
	if len(pathResults) == 0 || !hasReason(pathResults[0].Reasons, retrieval.ReasonExactPath) {
		t.Fatalf("expected exact path reason, got %#v", pathResults)
	}

	ordered, err := store.Search(retrieval.SearchRequest{Query: "Helper"})
	if err != nil {
		t.Fatalf("Search(lexical) error = %v", err)
	}
	if len(ordered) < 4 {
		t.Fatalf("expected at least 4 results, got %d", len(ordered))
	}
	if ordered[0].Path != "internal/order/service.go" {
		t.Fatalf("expected exact symbol first, got %#v", ordered[0])
	}
	if !hasReason(ordered[0].Reasons, retrieval.ReasonExactSymbol) {
		t.Fatalf("expected exact symbol reason, got %#v", ordered[0])
	}
	if ordered[1].Path != "internal/order/identifier.go" || !hasReason(ordered[1].Reasons, retrieval.ReasonIdentifier) {
		t.Fatalf("expected identifier second, got %#v", ordered[1])
	}
	if ordered[2].Path != "internal/lexical/common.go" || ordered[2].LineStart != 3 || !hasReason(ordered[2].Reasons, retrieval.ReasonLexical) {
		t.Fatalf("expected lexical third, got %#v", ordered[2])
	}
	if ordered[3].Path != "internal/lexical/common.go" || ordered[3].LineStart != 3 || !hasReason(ordered[3].Reasons, retrieval.ReasonLexical) {
		t.Fatalf("expected lexical fourth, got %#v", ordered[3])
	}
	if len(ordered[0].Provenance) == 0 || len(ordered[1].Provenance) == 0 || len(ordered[2].Provenance) == 0 || len(ordered[3].Provenance) == 0 {
		t.Fatalf("expected provenance fallback set")
	}

	orderedRepeat, err := store.Search(retrieval.SearchRequest{Query: "Helper"})
	if err != nil {
		t.Fatalf("Search(lexical repeat) error = %v", err)
	}
	if len(orderedRepeat) < 4 {
		t.Fatalf("expected at least 4 repeat results, got %d", len(orderedRepeat))
	}
	if ordered[2].Path != orderedRepeat[2].Path ||
		ordered[2].LineStart != orderedRepeat[2].LineStart ||
		ordered[2].ChunkID != orderedRepeat[2].ChunkID {
		t.Fatalf("expected deterministic lexical tie path/line/chunk for 3rd result, got %#v then %#v", ordered[2], orderedRepeat[2])
	}
	if ordered[3].Path != orderedRepeat[3].Path ||
		ordered[3].LineStart != orderedRepeat[3].LineStart ||
		ordered[3].ChunkID != orderedRepeat[3].ChunkID {
		t.Fatalf("expected deterministic lexical tie path/line/chunk for 4th result, got %#v then %#v", ordered[3], orderedRepeat[3])
	}
}

func TestWeightedExactTermsBoostAndExplain(t *testing.T) {
	store, err := Create(filepath.Join(t.TempDir(), "lexical"), uuid.NewString(), uuid.NewString(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	docs := []retrieval.Document{
		{ID: "path", Path: "internal/order/service.go", Content: "ordinary order content", Symbol: "Service.CreateOrder", LineStart: 1, LineEnd: 2, ContentHash: "a", Provenance: "test", Identifiers: []string{"order"}},
		{ID: "decoy", Path: "internal/order/other.go", Content: "ordinary order content", Symbol: "Service.Other", LineStart: 1, LineEnd: 2, ContentHash: "b", Provenance: "test", Identifiers: []string{"order"}},
	}
	for index := range docs {
		docs[index].ID = retrieval.DeterministicChunkID(docs[index].Path, docs[index].ContentHash, docs[index].LineStart, docs[index].LineEnd)
	}
	if err := store.Apply(ApplyRequest{ProjectID: store.ProjectID(), EpochID: store.EpochID(), Add: docs}); err != nil {
		t.Fatal(err)
	}

	results, err := store.Search(retrieval.SearchRequest{
		Query: "order",
		WeightedTerms: []retrieval.WeightedSearchTerm{
			{Value: "order", Weight: 1},
			{Value: "internal/order/service.go", Weight: 3, Exact: true},
			{Value: "Service.CreateOrder", Weight: 3, Exact: true},
			{Value: "internal/order/other.go", Weight: 1, Exact: true},
			{Value: "Service.Other", Weight: 1, Exact: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].Path != "internal/order/service.go" {
		t.Fatalf("weighted exact ranking = %#v", results)
	}
	if !hasReason(results[0].Reasons, retrieval.ReasonExactPath) || !hasReason(results[0].Reasons, retrieval.ReasonExactSymbol) {
		t.Fatalf("weighted exact reasons = %#v", results[0].Reasons)
	}
	if !containsString(results[0].MatchedTerms, "internal/order/service.go") || !containsString(results[0].MatchedTerms, "service.createorder") {
		t.Fatalf("weighted exact matched terms = %#v", results[0].MatchedTerms)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("weighted exact score did not preserve field priority: %#v", results)
	}
}

func TestIndexstoreCorpusDiscoveryAndQuery(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "indexing", "corpus")
	projectID := uuid.NewString()
	epochID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "lexical")

	store, err := Create(path, projectID, epochID, 7)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer store.Close()

	docs, err := corpusDocuments(root)
	if err != nil {
		t.Fatalf("corpusDocuments() error = %v", err)
	}
	if len(docs) == 0 {
		t.Fatalf("expected corpus docs")
	}

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: docs}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	orderPathResult, err := store.Search(retrieval.SearchRequest{Query: "internal/order/service.go"})
	if err != nil {
		t.Fatalf("Search(order service) error = %v", err)
	}
	if len(orderPathResult) == 0 || orderPathResult[0].Path != "internal/order/service.go" {
		t.Fatalf("expected internal/order/service.go as first hit, got %#v", orderPathResult)
	}

	orderQueryResult, err := store.Search(retrieval.SearchRequest{Query: "CreateOrder"})
	if err != nil {
		t.Fatalf("Search(order query) error = %v", err)
	}
	if len(orderQueryResult) == 0 || orderQueryResult[0].Path != "internal/order/service.go" {
		t.Fatalf("expected order query to prioritize internal/order/service.go, got %#v", orderQueryResult)
	}

	orderQueryResultRepeat, err := store.Search(retrieval.SearchRequest{Query: "CreateOrder"})
	if err != nil {
		t.Fatalf("Search(order query repeat) error = %v", err)
	}
	if len(orderQueryResultRepeat) == 0 || orderQueryResultRepeat[0].Path != "internal/order/service.go" {
		t.Fatalf("expected order query repeatability to keep internal/order/service.go first, got %#v", orderQueryResultRepeat)
	}

	orderTaskResult, err := store.Search(retrieval.SearchRequest{Query: "existing order Charge"})
	if err != nil {
		t.Fatalf("Search(order task) error = %v", err)
	}
	if !pathInTop(orderTaskResult, "internal/order/service.go", 3) {
		t.Fatalf("expected task-like order query to rank service.go in top 3, got %#v", orderTaskResult)
	}

	notFoundResult, err := store.Search(retrieval.SearchRequest{Query: "ErrNotFound"})
	if err != nil {
		t.Fatalf("Search(ErrNotFound) error = %v", err)
	}
	if len(notFoundResult) == 0 || notFoundResult[0].Path != "internal/order/repository.go" {
		t.Fatalf("expected ErrNotFound query to prioritize internal/order/repository.go, got %#v", notFoundResult)
	}

	http404Result, err := store.Search(retrieval.SearchRequest{Query: "HTTP 404"})
	if err != nil {
		t.Fatalf("Search(HTTP 404) error = %v", err)
	}
	if len(http404Result) == 0 || http404Result[0].Path != "internal/httpapi/order_handler.go" {
		t.Fatalf("expected HTTP 404 query to prioritize internal/httpapi/order_handler.go, got %#v", http404Result)
	}

	httpTaskResult, err := store.Search(retrieval.SearchRequest{Query: "ErrNotFound StatusNotFound"})
	if err != nil {
		t.Fatalf("Search(HTTP task) error = %v", err)
	}
	if !pathInTop(httpTaskResult, "internal/httpapi/order_handler.go", 3) {
		t.Fatalf("expected task-like HTTP query to rank order_handler.go in top 3, got %#v", httpTaskResult)
	}

	refundResult, err := store.Search(retrieval.SearchRequest{Query: "refund"})
	if err != nil {
		t.Fatalf("Search(refund) error = %v", err)
	}
	if len(refundResult) == 0 || !containsPaths(refundResult, "web/src/refunds/refund-service.ts", "web/src/refunds/refund-controller.ts") {
		t.Fatalf("expected refund query results in both service and controller, got %#v", refundResult)
	}
	refundCreate, err := store.Search(retrieval.SearchRequest{Query: "createRefund"})
	if err != nil {
		t.Fatalf("Search(createRefund) error = %v", err)
	}
	if len(refundCreate) == 0 || refundCreate[0].Path != "web/src/refunds/refund-service.ts" {
		t.Fatalf("expected createRefund query to prioritize web/src/refunds/refund-service.ts, got %#v", refundCreate)
	}
	refundCreateRepeat, err := store.Search(retrieval.SearchRequest{Query: "createRefund"})
	if err != nil {
		t.Fatalf("Search(createRefund repeat) error = %v", err)
	}
	if len(refundCreateRepeat) == 0 || refundCreateRepeat[0].Path != "web/src/refunds/refund-service.ts" {
		t.Fatalf("expected createRefund repeatability to keep web/src/refunds/refund-service.ts first, got %#v", refundCreateRepeat)
	}

	refundTaskResult, err := store.Search(retrieval.SearchRequest{Query: "findByRequestId save sendRefundCreated"})
	if err != nil {
		t.Fatalf("Search(refund task) error = %v", err)
	}
	if !pathInTop(refundTaskResult, "web/src/refunds/refund-service.ts", 3) {
		t.Fatalf("expected task-like refund query to rank refund-service.ts in top 3, got %#v", refundTaskResult)
	}

	controllerTaskResult, err := store.Search(retrieval.SearchRequest{Query: "postRefund createRefund status"})
	if err != nil {
		t.Fatalf("Search(controller task) error = %v", err)
	}
	if !pathInTop(controllerTaskResult, "web/src/refunds/refund-controller.ts", 3) {
		t.Fatalf("expected task-like controller query to rank refund-controller.ts in top 3, got %#v", controllerTaskResult)
	}

	for _, hit := range refundResult {
		if hit.Path == "ignored/generated-refund-index.ts" {
			t.Fatalf("ignored file should not be indexed")
		}
	}
}

func TestApplyRejectsDuplicateChunkInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lexical")
	projectID := uuid.NewString()
	epochID := uuid.NewString()

	store, err := Create(path, projectID, epochID, 1)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer store.Close()

	doc := retrieval.Document{
		Path:               "internal/dup.go",
		LineStart:          1,
		LineEnd:            1,
		Content:            "func duplicate() {}",
		ContentHash:        hashString("dup"),
		Snippet:            "func duplicate() {}",
		Provenance:         "fallback",
		Symbol:             "Duplicate",
		ChunkDiscriminator: "",
	}
	doc.ID = retrieval.DeterministicChunkID(doc.Path, doc.ContentHash, doc.LineStart, doc.LineEnd)
	doc.SymbolNormalized = strings.ToLower(doc.Symbol)

	if err := store.Apply(ApplyRequest{ProjectID: projectID, EpochID: epochID, Add: []retrieval.Document{doc, doc}}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("expected incompatible on duplicate chunk input, got %v", err)
	}
}

func chunksFromPath(path, language string, content []byte) ([]retrieval.Document, error) {
	h := sha256.Sum256(content)
	docs, err := chunker.Chunks(path, language, content, hex.EncodeToString(h[:]), nil)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		docs[i].Path = retrieval.NormalizePath(docs[i].Path)
		docs[i].Language = language
		docs[i].Identifiers = retrieval.NormalizeAndExpandIdentifiers(docs[i].Content)
		if docs[i].SymbolNormalized == "" {
			docs[i].SymbolNormalized = strings.ToLower(docs[i].Symbol)
		}
	}
	return docs, nil
}

func corpusDocuments(root string) ([]retrieval.Document, error) {
	files := make([]retrieval.Document, 0)

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirPath := filepath.Join(root, entry.Name())
			err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if strings.Contains(filepath.ToSlash(path), "/ignored/") {
						return filepath.SkipDir
					}
					return nil
				}

				if filepath.Ext(path) != ".go" && filepath.Ext(path) != ".ts" {
					return nil
				}
				if strings.HasSuffix(filepath.ToSlash(path), "ignored/generated-refund-index.ts") {
					return nil
				}

				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				chunked, err := chunksFromPath(filepath.ToSlash(rel), discovery.LanguageForPath(path), content)
				if err != nil {
					return err
				}
				files = append(files, chunked...)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}

	return files, nil
}

func hashString(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func chunkIDsFromDocs(docs []retrieval.Document) []string {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	sort.Strings(ids)
	return ids
}

func chunkIDsFromSearchResult(store *LexicalStore, t *testing.T, path string) []string {
	t.Helper()

	results, err := store.Search(retrieval.SearchRequest{Query: path})
	if err != nil {
		t.Fatalf("Search(%q) error = %v", path, err)
	}

	ids := make([]string, 0, len(results))
	for _, result := range results {
		if result.Path == path {
			ids = append(ids, result.ChunkID)
		}
	}
	sort.Strings(ids)
	return ids
}

func equalStringSets(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	seen := make(map[string]int, len(first))
	for _, id := range first {
		seen[id]++
	}
	for _, id := range second {
		count, ok := seen[id]
		if !ok {
			return false
		}
		if count == 1 {
			delete(seen, id)
		} else {
			seen[id] = count - 1
		}
	}
	return len(seen) == 0
}

func hasReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}

func containsPaths(results []retrieval.SearchResult, paths ...string) bool {
	seen := make(map[string]struct{}, len(results))
	for _, path := range paths {
		seen[path] = struct{}{}
	}

	pathsFound := make(map[string]struct{}, len(results))
	for _, result := range results {
		if _, ok := seen[result.Path]; ok {
			pathsFound[result.Path] = struct{}{}
		}
	}

	for path := range seen {
		if _, ok := pathsFound[path]; !ok {
			return false
		}
	}
	return true
}

func pathInTop(results []retrieval.SearchResult, path string, limit int) bool {
	if limit > len(results) {
		limit = len(results)
	}
	for _, result := range results[:limit] {
		if result.Path == path {
			return true
		}
	}

	return false
}
