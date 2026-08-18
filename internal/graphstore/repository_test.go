package graphstore

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/vtimame/istok.sh/internal/codegraph"
)

const (
	testPathA = "internal/a.go"
	testPathB = "internal/b.go"
)

func TestInitializeCreatesGraphSchema(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", t.TempDir()+"/graph.sqlite")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Initialize(db); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	defer db.Close()

	tables := []string{"graph_nodes", "graph_edges", "graph_diagnostics"}
	for _, table := range tables {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			t.Fatalf("query table %q: %v", table, err)
		}
		if exists != 1 {
			t.Fatalf("expected table %q", table)
		}
	}

	var fkEnabled string
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fkEnabled); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fkEnabled != "on" && fkEnabled != "1" {
		t.Fatalf("expected foreign keys enabled, got %q", fkEnabled)
	}

	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list(graph_edges)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list(graph_edges): %v", err)
	}
	defer rows.Close()

	foundSource := false
	foundTarget := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete string
		var match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		if table == "graph_nodes" && from == "source_id" && to == "id" && onDelete == "CASCADE" {
			foundSource = true
		}
		if table == "graph_nodes" && from == "target_id" && to == "id" && onDelete == "SET NULL" {
			foundTarget = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign key rows: %v", err)
	}
	if !foundSource || !foundTarget {
		t.Fatalf("expected source cascade and target nullification; got source=%t target=%t", foundSource, foundTarget)
	}
}

func TestLookupNodesByPathRangeAndNeighborsWithMetadata(t *testing.T) {
	ctx := context.Background()
	db := openGraphDB(t)
	if err := Initialize(db); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	for _, values := range [][]any{{"source", "function", "go", testPathA, "Source", "pkg.Source", "", 4, 10, "a"}, {"target", "function", "go", testPathB, "Target", "pkg.Target", "", 1, 3, "b"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO graph_nodes(id,kind,language,path,name,qualified_name,signature,line_start,line_end,content_hash) VALUES(?,?,?,?,?,?,?,?,?,?)`, values...); err != nil {
			t.Fatal(err)
		}
	}
	for _, edge := range [][]any{
		{"source", "target", "pkg.Target", "contains", "resolved", 1.0, testPathA, 6},
		{"source", "target", "pkg.Target", "references", "resolved", .9, testPathA, 7},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO graph_edges(source_id,target_id,target_name,kind,provenance,confidence,evidence_path,evidence_line) VALUES(?,?,?,?,?,?,?,?)`, edge...); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := repo.LookupNodesByPathRange(ctx, GraphPathRangeRequest{Path: testPathA, LineStart: 5, LineEnd: 5})
	if err != nil || len(nodes) != 1 || nodes[0].ID != "source" {
		t.Fatalf("range lookup = %#v, %v", nodes, err)
	}
	neighbors, err := repo.NeighborsWithMetadata(ctx, NeighborsWithMetadataRequest{SourceID: "source", Limit: 1})
	if err != nil || len(neighbors) != 1 || neighbors[0].Kind != "references" || neighbors[0].Provenance != "resolved" {
		t.Fatalf("metadata neighbors = %#v, %v", neighbors, err)
	}
}

func TestReplacePerformsStaleCleanupChangeDeleteRenameAndReResolve(t *testing.T) {
	ctx := context.Background()
	db := openGraphDB(t)
	repo := New(db)

	if err := Initialize(db); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{testPathA, testPathB}, []codegraph.FileGraph{
			{
				Path:        testPathA,
				ContentHash: "old-a",
				Nodes: []codegraph.Node{
					{
						ID:            "node-a-1",
						Kind:          "type",
						Language:      "go",
						Path:          testPathA,
						Name:          "Target",
						QualifiedName: "pkg.Target",
						Signature:     "type Target struct{}",
						LineStart:     1,
						LineEnd:       2,
						ContentHash:   "old-a",
					},
				},
				Diagnostics: []codegraph.Diagnostic{
					{Path: testPathA, Language: "go", Line: 1, Message: "old a diag"},
				},
			},
			{
				Path:        testPathB,
				ContentHash: "b",
				Nodes: []codegraph.Node{
					{
						ID:            "node-b",
						Kind:          "function",
						Language:      "go",
						Path:          testPathB,
						Name:          "Caller",
						QualifiedName: "pkg.Caller",
						Signature:     "func Caller()",
						LineStart:     1,
						LineEnd:       1,
						ContentHash:   "b",
					},
				},
				Edges: []codegraph.Edge{
					{
						SourceID:     "node-b",
						TargetName:   "pkg.Target",
						Kind:         "references",
						EvidencePath: testPathB,
						EvidenceLine: 3,
					},
				},
			},
		})
	}); err != nil {
		t.Fatalf("initial replace error = %v", err)
	}

	initialDiag, err := repo.ListDiagnostics(ctx)
	if err != nil {
		t.Fatalf("ListDiagnostics() error = %v", err)
	}
	if len(initialDiag) != 1 || initialDiag[0].Message != "old a diag" {
		t.Fatalf("unexpected initial diagnostics: %#v", initialDiag)
	}

	neighbors, err := repo.Neighbors(ctx, NeighborsRequest{SourceID: "node-b"})
	if err != nil {
		t.Fatalf("neighbors initial error = %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].ID != "node-a-1" {
		t.Fatalf("expected node-a-1 neighbor, got %#v", neighbors)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{testPathA}, []codegraph.FileGraph{
			{
				Path:        testPathA,
				ContentHash: "new-a",
				Nodes: []codegraph.Node{
					{
						ID:            "node-a-2",
						Kind:          "type",
						Language:      "go",
						Path:          testPathA,
						Name:          "Target",
						QualifiedName: "pkg.Target",
						Signature:     "type Target struct{}",
						LineStart:     1,
						LineEnd:       2,
						ContentHash:   "new-a",
					},
				},
				Diagnostics: []codegraph.Diagnostic{
					{Path: testPathA, Language: "go", Line: 1, Message: "new a diag"},
				},
			},
		})
	}); err != nil {
		t.Fatalf("change replace error = %v", err)
	}

	neighbors, err = repo.Neighbors(ctx, NeighborsRequest{SourceID: "node-b"})
	if err != nil {
		t.Fatalf("neighbors changed error = %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].ID != "node-a-2" {
		t.Fatalf("expected re-resolved neighbor node-a-2, got %#v", neighbors)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{testPathA}, nil)
	}); err != nil {
		t.Fatalf("delete replace error = %v", err)
	}

	rows := mustQueryInt(t, ctx, db, `SELECT count(*) FROM graph_nodes`)
	if got := rows; got != 1 {
		t.Fatalf("expected 1 node after delete replace, got %d", got)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{"internal/old_a.go"}, []codegraph.FileGraph{
			{
				Path:        "internal/new_a.go",
				ContentHash: "renamed-a",
				Nodes: []codegraph.Node{
					{
						ID:            "node-a-3",
						Kind:          "type",
						Language:      "go",
						Path:          "internal/new_a.go",
						Name:          "Target",
						QualifiedName: "pkg.Target",
						Signature:     "type Target struct{}",
						LineStart:     1,
						LineEnd:       2,
						ContentHash:   "renamed-a",
					},
				},
			},
		})
	}); err != nil {
		t.Fatalf("rename replace error = %v", err)
	}

	mustNoNode(t, ctx, db, "node-a-1")
	mustNoNode(t, ctx, db, "node-a-2")
	node := mustNode(t, ctx, db, "node-a-3")
	if node.Path != "internal/new_a.go" {
		t.Fatalf("expected renamed path, got %q", node.Path)
	}
}

func TestResolveAmbiguousShortNameLeavesUnresolved(t *testing.T) {
	ctx := context.Background()
	db := openGraphDB(t)
	repo := New(db)
	if err := Initialize(db); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{testPathA, testPathB}, []codegraph.FileGraph{
			{
				Path:        testPathA,
				ContentHash: "a",
				Nodes: []codegraph.Node{
					{
						ID:            "ambiguous-type-a",
						Kind:          "type",
						Language:      "go",
						Path:          testPathA,
						Name:          "Type",
						QualifiedName: "pkg.alpha.Type",
						Signature:     "type Type struct{}",
						LineStart:     1,
						LineEnd:       1,
						ContentHash:   "a",
					},
				},
			},
			{
				Path:        testPathB,
				ContentHash: "b",
				Nodes: []codegraph.Node{
					{
						ID:            "ambiguous-type-b",
						Kind:          "type",
						Language:      "go",
						Path:          testPathB,
						Name:          "Type",
						QualifiedName: "pkg.beta.Type",
						Signature:     "type Type struct{}",
						LineStart:     1,
						LineEnd:       1,
						ContentHash:   "b",
					},
					{
						ID:            "source",
						Kind:          "function",
						Language:      "go",
						Path:          testPathB,
						Name:          "Caller",
						QualifiedName: "pkg.Caller",
						Signature:     "func Caller()",
						LineStart:     3,
						LineEnd:       3,
						ContentHash:   "b",
					},
				},
				Edges: []codegraph.Edge{
					{
						SourceID:     "source",
						TargetName:   "Type",
						Kind:         "references",
						EvidencePath: testPathB,
						EvidenceLine: 3,
					},
				},
			},
		})
	}); err != nil {
		t.Fatalf("Replace error = %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT target_id FROM graph_edges`)
	if err != nil {
		t.Fatalf("query edge target: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetID sql.NullString
		if err := rows.Scan(&targetID); err != nil {
			t.Fatalf("scan edge target: %v", err)
		}
		if targetID.Valid {
			t.Fatalf("expected unresolved edge, got target id %q", targetID.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate edges: %v", err)
	}
}

func TestLookupNeighborsAndPathsRespectBounds(t *testing.T) {
	ctx := context.Background()
	db := openGraphDB(t)
	repo := New(db)
	if err := Initialize(db); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{"internal/a.go", "internal/b.go", "internal/c.go", "internal/d.go"}, []codegraph.FileGraph{
			{
				Path:        "internal/a.go",
				ContentHash: "a",
				Nodes: []codegraph.Node{
					{ID: "nA", Kind: "type", Language: "go", Path: "internal/a.go", Name: "A", QualifiedName: "pkg.A", Signature: "A", LineStart: 1, LineEnd: 1, ContentHash: "a"},
				},
				Edges: []codegraph.Edge{
					{SourceID: "nA", TargetName: "pkg.B", Kind: "references", EvidencePath: "internal/a.go", EvidenceLine: 2},
				},
			},
			{
				Path:        "internal/b.go",
				ContentHash: "b",
				Nodes: []codegraph.Node{
					{ID: "nB", Kind: "type", Language: "go", Path: "internal/b.go", Name: "B", QualifiedName: "pkg.B", Signature: "B", LineStart: 1, LineEnd: 1, ContentHash: "b"},
					{ID: "nC", Kind: "type", Language: "go", Path: "internal/b.go", Name: "C", QualifiedName: "pkg.C", Signature: "C", LineStart: 2, LineEnd: 2, ContentHash: "b"},
				},
				Edges: []codegraph.Edge{
					{SourceID: "nB", TargetName: "pkg.C", Kind: "references", EvidencePath: "internal/b.go", EvidenceLine: 4},
					{SourceID: "nC", TargetName: "pkg.D", Kind: "references", EvidencePath: "internal/b.go", EvidenceLine: 5},
				},
			},
			{
				Path:        "internal/c.go",
				ContentHash: "c",
				Nodes: []codegraph.Node{
					{ID: "nD", Kind: "type", Language: "go", Path: "internal/c.go", Name: "D", QualifiedName: "pkg.D", Signature: "D", LineStart: 1, LineEnd: 1, ContentHash: "c"},
					{ID: "nE", Kind: "type", Language: "go", Path: "internal/c.go", Name: "E", QualifiedName: "pkg.E", Signature: "E", LineStart: 2, LineEnd: 2, ContentHash: "c"},
				},
				Edges: []codegraph.Edge{
					{SourceID: "nD", TargetName: "pkg.F", Kind: "references", EvidencePath: "internal/c.go", EvidenceLine: 3},
				},
			},
			{
				Path:        "internal/d.go",
				ContentHash: "d",
				Nodes: []codegraph.Node{
					{ID: "nF", Kind: "type", Language: "go", Path: "internal/d.go", Name: "F", QualifiedName: "pkg.F", Signature: "F", LineStart: 1, LineEnd: 1, ContentHash: "d"},
				},
			},
		})
	}); err != nil {
		t.Fatalf("Replace error = %v", err)
	}

	nodes, err := repo.LookupSymbols(ctx, LookupSymbolsRequest{
		Name:  "C",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("LookupSymbols() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "nC" {
		t.Fatalf("expected bounded lookup result nC only, got %#v", nodes)
	}

	sameName, err := repo.LookupSymbols(ctx, LookupSymbolsRequest{
		QualifiedName: "pkg.C",
	})
	if err != nil {
		t.Fatalf("LookupSymbols(qualified) error = %v", err)
	}
	if len(sameName) != 1 || sameName[0].ID != "nC" {
		t.Fatalf("expected qualified match nC, got %#v", sameName)
	}

	neighbors, err := repo.Neighbors(ctx, NeighborsRequest{
		SourceID: "nD",
		Limit:    5,
	})
	if err != nil {
		t.Fatalf("Neighbors() error = %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].ID != "nF" {
		t.Fatalf("expected neighbor nF, got %#v", neighbors)
	}

	paths, err := repo.Paths(ctx, PathsRequest{
		From:       "nA",
		To:         "nF",
		MaxDepth:   4,
		MaxResults: 1,
	})
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}
	if len(paths) != 1 || len(paths[0]) != 5 || paths[0][1].ID != "nB" || paths[0][2].ID != "nC" || paths[0][3].ID != "nD" || paths[0][4].ID != "nF" {
		t.Fatalf("unexpected shortest path: %#v", paths)
	}

	depthLimited, err := repo.Path(ctx, PathRequest{
		From:     "nA",
		To:       "nF",
		MaxDepth: 3,
	})
	if len(depthLimited) != 0 {
		t.Fatalf("expected no path at maxDepth=3, got %#v", depthLimited)
	}
	if err != nil {
		t.Fatalf("Path(maxDepth=3) error = %v", err)
	}
}

func TestListDiagnosticsSorted(t *testing.T) {
	ctx := context.Background()
	db := openGraphDB(t)
	repo := New(db)
	if err := Initialize(db); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := runTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		return repo.Replace(ctx, tx, []string{"x.go", "y.go"}, []codegraph.FileGraph{
			{
				Path:        "x.go",
				ContentHash: "x",
				Diagnostics: []codegraph.Diagnostic{
					{Path: "x.go", Language: "go", Line: 20, Message: "second"},
					{Path: "x.go", Language: "go", Line: 10, Message: "first"},
				},
			},
			{
				Path:        "y.go",
				ContentHash: "y",
				Diagnostics: []codegraph.Diagnostic{
					{Path: "y.go", Language: "go", Line: 1, Message: "alpha"},
				},
			},
		})
	}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	diagnostics, err := repo.ListDiagnostics(ctx)
	if err != nil {
		t.Fatalf("ListDiagnostics() error = %v", err)
	}
	if len(diagnostics) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(diagnostics))
	}
	if diagnostics[0].Path != "x.go" || diagnostics[0].Line != 10 || diagnostics[0].Message != "first" {
		t.Fatalf("expected sorted first diagnostic, got %#v", diagnostics[0])
	}
	if diagnostics[1].Line != 20 {
		t.Fatalf("expected second diagnostic, got %#v", diagnostics[1])
	}
	if diagnostics[2].Path != "y.go" || diagnostics[2].Message != "alpha" {
		t.Fatalf("expected y.go diagnostic last, got %#v", diagnostics[2])
	}
}

func openGraphDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", t.TempDir()+"/graph.sqlite")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func runTx(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func mustNode(t *testing.T, ctx context.Context, db *sql.DB, id string) codegraph.Node {
	t.Helper()

	row := db.QueryRowContext(ctx, `SELECT id, kind, language, path, name, qualified_name, signature, line_start, line_end, content_hash FROM graph_nodes WHERE id = ?`, id)
	var n codegraph.Node
	if err := row.Scan(
		&n.ID,
		&n.Kind,
		&n.Language,
		&n.Path,
		&n.Name,
		&n.QualifiedName,
		&n.Signature,
		&n.LineStart,
		&n.LineEnd,
		&n.ContentHash,
	); err != nil {
		t.Fatalf("scan node %q: %v", id, err)
	}
	return n
}

func mustNoNode(t *testing.T, ctx context.Context, db *sql.DB, id string) {
	t.Helper()

	var idValue sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT id FROM graph_nodes WHERE id = ?`, id).Scan(&idValue); err != nil && err != sql.ErrNoRows {
		t.Fatalf("query node %q: %v", id, err)
	}
	if idValue.Valid {
		t.Fatalf("expected node %q to be removed", id)
	}
}

func mustQueryInt(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil {
		t.Fatalf("query int %q: %v", query, err)
	}
	return value
}
