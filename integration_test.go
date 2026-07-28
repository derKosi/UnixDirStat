package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestLargeDir tests scanning a real directory with many files.
func TestLargeDir(t *testing.T) {
	// Scan a known real directory (the test data we know exists)
	tmp := t.TempDir()

	// Create a realistic structure
	dirs := []string{
		"src", "src/cmd", "src/lib", "docs", "tests",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(tmp, d), 0755)
	}

	files := map[string]int{
		"src/main.go":     500,
		"src/cmd/run.go":  2000,
		"src/lib/util.go": 1500,
		"src/lib/db.go":   3000,
		"docs/readme.md":  400,
		"docs/api.md":     800,
		"tests/main_test": 1200,
		"Makefile":        100,
		"go.mod":          50,
		".gitignore":      20,
	}

	for name, size := range files {
		os.WriteFile(filepath.Join(tmp, name), make([]byte, size), 0644)
	}

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	if !s.Stats.Done.Load() {
		t.Fatal("scan should be done")
	}

	filesCount := s.Stats.FilesScanned.Load()
	if filesCount != int64(len(files)) {
		t.Errorf("expected %d files, got %d", len(files), filesCount)
	}

	var expectedTotal int64
	for _, size := range files {
		expectedTotal += int64(size)
	}
	if s.RootNode.Size != expectedTotal {
		t.Errorf("expected total size %d, got %d", expectedTotal, s.RootNode.Size)
	}

	// Test extensions
	exts := GroupByExtension(s.RootNode, true)
	if len(exts) == 0 {
		t.Fatal("should have extensions")
	}
	// .go should be biggest (500+2000+1500+3000 = 7000)
	if exts[0].Ext != ".go" {
		t.Errorf("expected .go as biggest extension, got %s (size=%d)", exts[0].Ext, exts[0].Size)
	}
	if exts[0].Size != 7000 {
		t.Errorf("expected .go size 7000, got %d", exts[0].Size)
	}

	// Test treemap layout
	items := BuildTreemapItems(s.RootNode)
	layoutItems := Squarify(items, Rect{0, 0, 200, 50})

	totalArea := 0
	for _, item := range layoutItems {
		if item.Rect.W > 0 && item.Rect.H > 0 {
			totalArea += item.Rect.W * item.Rect.H
		}
	}
	if totalArea != 200*50 {
		t.Errorf("treemap should fill area %d, got %d", 200*50, totalArea)
	}

	// Test tree expansion
	s.RootNode.Expanded = true
	nodes := FlattenTree(s.RootNode, maxTreeDepth, true, sortBySize)
	fmt.Printf("  Tree has %d visible nodes (expanded root)\n", len(nodes))

	// Expand src/
	for _, n := range nodes {
		if n.Node.Name == "src" {
			n.Node.Expanded = true
		}
	}
	nodes2 := FlattenTree(s.RootNode, maxTreeDepth, true, sortBySize)
	fmt.Printf("  Tree has %d visible nodes (expanded src)\n", len(nodes2))
	if len(nodes2) <= len(nodes) {
		t.Error("expanding src should show more nodes")
	}
}

// TestHiddenFiles verifies hidden files are scanned.
func TestHiddenFiles(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, ".hidden"), make([]byte, 42), 0644)
	os.WriteFile(filepath.Join(tmp, "visible"), make([]byte, 100), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	if s.Stats.FilesScanned.Load() != 2 {
		t.Errorf("expected 2 files (hidden+visible), got %d", s.Stats.FilesScanned.Load())
	}
	if s.RootNode.Size != 142 {
		t.Errorf("expected 142 bytes, got %d", s.RootNode.Size)
	}
}

// TestEmptyDir handles empty directories.
func TestEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	if s.Stats.FilesScanned.Load() != 0 {
		t.Errorf("expected 0 files, got %d", s.Stats.FilesScanned.Load())
	}
	if s.RootNode.Size != 0 {
		t.Errorf("expected 0 size, got %d", s.RootNode.Size)
	}

	exts := GroupByExtension(s.RootNode, true)
	if len(exts) != 0 {
		t.Errorf("expected 0 extensions, got %d", len(exts))
	}
}

// TestNoExt handles files without extension.
func TestNoExt(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "Makefile"), make([]byte, 100), 0644)
	os.WriteFile(filepath.Join(tmp, "README"), make([]byte, 200), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	exts := GroupByExtension(s.RootNode, true)
	if len(exts) != 1 {
		t.Fatalf("expected 1 extension group (none), got %d", len(exts))
	}
	if exts[0].Ext != "(none)" {
		t.Errorf("expected (none) extension, got %s", exts[0].Ext)
	}
}
