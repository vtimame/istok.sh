package retrieval

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type fakeLexical struct {
	direct []SearchResult
	ranges map[string][]SearchResult
}

func (f fakeLexical) Search(SearchRequest) ([]SearchResult, error) {
	return append([]SearchResult(nil), f.direct...), nil
}
func (f fakeLexical) LookupByPathRange(path string, _, _ int) ([]SearchResult, error) {
	return append([]SearchResult(nil), f.ranges[path]...), nil
}

type fakeGraph struct {
	nodes     map[string][]GraphNode
	neighbors map[string][]GraphNeighbor
}

func (f fakeGraph) LookupNodesByPathRange(_ context.Context, request GraphPathRangeRequest) ([]GraphNode, error) {
	return append([]GraphNode(nil), f.nodes[request.Path]...), nil
}
func (f fakeGraph) NeighborsWithMetadata(_ context.Context, request NeighborsWithMetadataRequest) ([]GraphNeighbor, error) {
	return append([]GraphNeighbor(nil), f.neighbors[request.SourceID]...), nil
}

func testResult(path string, line int, score float64, snippet string) SearchResult {
	return SearchResult{ChunkID: path + string(rune(line)), Path: path, LineStart: line, LineEnd: line, Score: score, Snippet: snippet, Symbol: "Handle"}
}

func TestSearchTaskReservesDirectAndExplainsGraphSupport(t *testing.T) {
	direct := []SearchResult{testResult("a.go", 1, 10, "refund direct"), testResult("b.go", 1, 9, "refund direct")}
	lexical := fakeLexical{direct: direct, ranges: map[string][]SearchResult{"support.go": {testResult("support.go", 3, 1, "save refund")}}}
	graph := fakeGraph{nodes: map[string][]GraphNode{"a.go": {{ID: "a", Path: "a.go", LineStart: 1, LineEnd: 1}}}, neighbors: map[string][]GraphNeighbor{"a": {{Node: GraphNode{ID: "s", Path: "support.go", Symbol: "Repository.Save", LineStart: 3, LineEnd: 3}, Kind: "calls", Provenance: "resolved", Confidence: 1}}}}
	got, err := SearchTaskWithOptions(context.Background(), lexical, graph, TaskSearchRequest{Title: "refund"}, TaskSearchOptions{ReserveDirect: 2, MaxItems: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("items = %d, want 3", len(got))
	}
	if got[0].Path != "a.go" || got[1].Path != "b.go" {
		t.Fatalf("reserved direct hits not retained: %#v", got)
	}
	if got[2].GraphScore == 0 || !strings.Contains(strings.Join(got[2].Reasons, " "), "graph neighbor") {
		t.Fatalf("graph explanation missing: %#v", got[2])
	}
	if got[2].GraphScore != 10*graphDecayFactor*edgeWeight("calls") {
		t.Fatalf("graph score = %f", got[2].GraphScore)
	}
	if len(got[2].MatchedTerms) != 0 || !hasString(got[2].Provenance, "graph:resolved") || !strings.Contains(got[2].Reasons[0], "calls [resolved]") {
		t.Fatalf("graph provenance contract = %#v", got[2])
	}
}

func TestTaskTermsPreserveFieldWeightsAndExactLiterals(t *testing.T) {
	terms, weighted := taskTerms(TaskSearchRequest{
		Title:              "alpha internal/title.go .github/workflows/release.yml Repository.FindByID.",
		Description:        "alpha",
		AcceptanceCriteria: "alpha",
		Notes:              "alpha internal/notes.go",
	})

	assertWeightedTerm(t, weighted, "alpha", 8, false)
	assertWeightedTerm(t, weighted, "internal/title.go", titleWeight, true)
	assertWeightedTerm(t, weighted, ".github/workflows/release.yml", titleWeight, true)
	assertWeightedTerm(t, weighted, "Repository.FindByID", titleWeight, true)
	assertWeightedTerm(t, weighted, "internal/notes.go", notesWeight, true)
	for _, exact := range []string{"internal/title.go", ".github/workflows/release.yml", "repository.findbyid", "internal/notes.go"} {
		if !hasString(terms, exact) {
			t.Fatalf("search terms do not preserve exact literal %q: %#v", exact, terms)
		}
	}
}

func TestSearchTaskBoundsGraphAndBudget(t *testing.T) {
	direct := []SearchResult{testResult("seed.go", 1, 5, "seed")}
	neighbors := make([]GraphNeighbor, 0, 4)
	ranges := map[string][]SearchResult{}
	for i := 0; i < 4; i++ {
		path := "n" + string(rune('a'+i)) + ".go"
		neighbors = append(neighbors, GraphNeighbor{Node: GraphNode{ID: path, Path: path, LineStart: 1, LineEnd: 1}, Kind: "calls"})
		ranges[path] = []SearchResult{testResult(path, 1, 1, strings.Repeat("x", 8))}
	}
	got, err := SearchTaskWithOptions(context.Background(), fakeLexical{direct: direct, ranges: ranges}, fakeGraph{nodes: map[string][]GraphNode{"seed.go": {{ID: "seed", Path: "seed.go", LineStart: 1, LineEnd: 1}}}, neighbors: map[string][]GraphNeighbor{"seed": neighbors}}, TaskSearchRequest{Title: "seed"}, TaskSearchOptions{MaxItems: 10, MaxNeighborsPerSeed: 2, MaxGraphOnly: 2, MaxTotalBytes: 11, MaxItemBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 1 {
		t.Fatalf("budget allowed %d items, want only direct item: %#v", len(got), got)
	}
}

func TestSearchTaskStableOrderAndWeighting(t *testing.T) {
	direct := []SearchResult{testResult("z.go", 2, 1, "refund"), testResult("a.go", 1, 1, "refund")}
	lexical := fakeLexical{direct: direct}
	graph := fakeGraph{nodes: map[string][]GraphNode{}}
	request := TaskSearchRequest{Title: "refund", Notes: "refund"}
	first, err := SearchTask(context.Background(), lexical, graph, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SearchTask(context.Background(), lexical, graph, request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("unstable result: %#v != %#v", first, second)
	}
	if first[0].Path != "a.go" || first[0].LexicalScore == 0 || len(first[0].MatchedTerms) == 0 {
		t.Fatalf("stable lexical result missing explanation: %#v", first)
	}
}

func TestSearchTaskBoundsNeighborsAcrossOverlappingNodesAndExcludesContains(t *testing.T) {
	direct := []SearchResult{testResult("seed.go", 1, 4, "seed")}
	ranges := map[string][]SearchResult{}
	neighbors := map[string][]GraphNeighbor{}
	for _, nodeID := range []string{"one", "two"} {
		neighbors[nodeID] = []GraphNeighbor{
			{Node: GraphNode{ID: "contains", Path: "contains.go", LineStart: 1, LineEnd: 1}, Kind: "contains"},
			{Node: GraphNode{ID: nodeID + "a", Path: nodeID + "a.go", LineStart: 1, LineEnd: 1}, Kind: "calls", Provenance: "resolved"},
			{Node: GraphNode{ID: nodeID + "b", Path: nodeID + "b.go", LineStart: 1, LineEnd: 1}, Kind: "imports", Provenance: "resolved"},
		}
		for _, suffix := range []string{"a", "b"} {
			ranges[nodeID+suffix+".go"] = []SearchResult{testResult(nodeID+suffix+".go", 1, 1, "support")}
		}
	}
	got, err := SearchTaskWithOptions(context.Background(), fakeLexical{direct: direct, ranges: ranges}, fakeGraph{nodes: map[string][]GraphNode{"seed.go": {{ID: "one", Path: "seed.go", LineStart: 1, LineEnd: 1}, {ID: "two", Path: "seed.go", LineStart: 1, LineEnd: 1}}}, neighbors: neighbors}, TaskSearchRequest{Title: "seed"}, TaskSearchOptions{MaxNeighborsPerSeed: 2, MaxGraphOnly: 6})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("global neighbor bound = %#v", got)
	}
	for _, result := range got {
		if result.Path == "contains.go" {
			t.Fatalf("contains entered expansion: %#v", got)
		}
	}
}

func TestSearchTaskReservesDirectAgainstHigherGraphScore(t *testing.T) {
	direct := []SearchResult{testResult("a.go", 1, 10, "seed"), testResult("b.go", 1, .01, "seed")}
	lexical := fakeLexical{direct: direct, ranges: map[string][]SearchResult{"graph.go": {testResult("graph.go", 1, 1, "support")}}}
	graph := fakeGraph{nodes: map[string][]GraphNode{"a.go": {{ID: "a", Path: "a.go", LineStart: 1, LineEnd: 1}}}, neighbors: map[string][]GraphNeighbor{"a": {{Node: GraphNode{ID: "g", Path: "graph.go", LineStart: 1, LineEnd: 1}, Kind: "calls", Provenance: "resolved"}}}}
	got, err := SearchTaskWithOptions(context.Background(), lexical, graph, TaskSearchRequest{Title: "seed"}, TaskSearchOptions{ReserveDirect: 2, MaxItems: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Path != "b.go" {
		t.Fatalf("reserved direct displaced: %#v", got)
	}
}

func TestSearchTaskDeduplicatesDirectAndGraphRanges(t *testing.T) {
	direct := []SearchResult{
		testResult("seed.go", 1, 5, "seed"),
		testResult("seed.go", 1, 4, "seed duplicate"),
		testResult("other.go", 2, 3, "seed"),
	}
	lexical := fakeLexical{
		direct: direct,
		ranges: map[string][]SearchResult{
			"support.go": {testResult("support.go", 3, 1, "support")},
		},
	}
	graph := fakeGraph{
		nodes: map[string][]GraphNode{
			"seed.go": {
				{ID: "seed-one", Path: "seed.go", LineStart: 1, LineEnd: 1},
				{ID: "seed-two", Path: "seed.go", LineStart: 1, LineEnd: 1},
			},
		},
		neighbors: map[string][]GraphNeighbor{
			"seed-one": {{Node: GraphNode{ID: "support", Path: "support.go", LineStart: 3, LineEnd: 3}, Kind: "calls", Provenance: "resolved"}},
			"seed-two": {{Node: GraphNode{ID: "support", Path: "support.go", LineStart: 3, LineEnd: 3}, Kind: "calls", Provenance: "resolved"}},
		},
	}

	got, err := SearchTask(context.Background(), lexical, graph, TaskSearchRequest{Title: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("deduplicated results = %#v", got)
	}
	for _, path := range []string{"seed.go", "other.go", "support.go"} {
		count := 0
		for _, result := range got {
			if result.Path == path {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("path %q count = %d in %#v", path, count, got)
		}
	}
}

func assertWeightedTerm(t *testing.T, terms []WeightedSearchTerm, value string, weight float64, exact bool) {
	t.Helper()

	for _, term := range terms {
		if term.Value != value {
			continue
		}
		if term.Weight != weight || term.Exact != exact {
			t.Fatalf("weighted term %q = %#v, want weight=%f exact=%t", value, term, weight, exact)
		}

		return
	}

	t.Fatalf("weighted term %q missing from %#v", value, terms)
}

func hasString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}

	return false
}
