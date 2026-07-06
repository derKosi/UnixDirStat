package main

import (
	"os"
	"testing"
)

func TestScannerBasic(t *testing.T) {
	// Create a temp dir with known structure
	tmp := t.TempDir()
	os.Mkdir(tmp+"/sub", 0755)
	os.WriteFile(tmp+"/a.txt", []byte("hello"), 0644)
	os.WriteFile(tmp+"/b.go", []byte("package main"), 0644)
	os.WriteFile(tmp+"/sub/c.txt", []byte("world"), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	if !s.Stats.Done.Load() {
		t.Fatal("scan should be done")
	}
	files := s.Stats.FilesScanned.Load()
	if files != 3 {
		t.Errorf("expected 3 files, got %d", files)
	}
	// 5 + 12 + 5 = 22 bytes
	if s.RootNode.Size != 22 {
		t.Errorf("expected root size 22, got %d", s.RootNode.Size)
	}
	if !s.RootNode.Expanded {
		// Just check it starts false (expand is a UI concern)
		t.Log("root not expanded (expected)")
	}
}

func TestGroupByExtension(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(tmp+"/a.txt", make([]byte, 100), 0644)
	os.WriteFile(tmp+"/b.txt", make([]byte, 200), 0644)
	os.WriteFile(tmp+"/c.go", make([]byte, 50), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	exts := GroupByExtension(s.RootNode)
	if len(exts) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(exts))
	}

	// .txt should be first (larger)
	if exts[0].Ext != ".txt" {
		t.Errorf("expected .txt first, got %s", exts[0].Ext)
	}
	if exts[0].Size != 300 {
		t.Errorf("expected .txt size 300, got %d", exts[0].Size)
	}
	if exts[0].Count != 2 {
		t.Errorf("expected .txt count 2, got %d", exts[0].Count)
	}
}

func TestSquarify(t *testing.T) {
	items := []TreemapItem{
		{Size: 60, Color: "#ff0000"},
		{Size: 30, Color: "#00ff00"},
		{Size: 10, Color: "#0000ff"},
	}

	result := Squarify(items, Rect{0, 0, 100, 50})
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	// Check that rects cover the full area
	totalArea := 0
	for _, item := range result {
		totalArea += item.Rect.W * item.Rect.H
	}
	if totalArea != 100*50 {
		t.Errorf("expected total area %d, got %d", 100*50, totalArea)
	}
}

func TestSquarifySingleItem(t *testing.T) {
	items := []TreemapItem{
		{Size: 100, Color: "#ff0000"},
	}
	result := Squarify(items, Rect{0, 0, 80, 40})
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].Rect != (Rect{0, 0, 80, 40}) {
		t.Errorf("expected full rect, got %+v", result[0].Rect)
	}
}

func TestFlattenTree(t *testing.T) {
	tmp := t.TempDir()
	os.Mkdir(tmp+"/sub", 0755)
	os.WriteFile(tmp+"/a.txt", make([]byte, 10), 0644)
	os.WriteFile(tmp+"/sub/b.txt", make([]byte, 20), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	// Expand root
	s.RootNode.Expanded = true
	nodes := FlattenTree(s.RootNode, 20)

	// Should have: root, a.txt, sub/, = 3
	if len(nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(nodes))
	}

	// Expand sub too
	for _, n := range nodes {
		if n.Node.Name == "sub" {
			n.Node.Expanded = true
		}
	}
	nodes2 := FlattenTree(s.RootNode, 20)
	// root + a.txt + sub/ + sub/b.txt = 4
	if len(nodes2) != 4 {
		t.Errorf("expected 4 nodes after expand, got %d", len(nodes2))
		for _, n := range nodes2 {
			t.Logf("  node: %s isDir=%v", n.Node.Name, n.Node.IsDir)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}
	for _, tt := range tests {
		got := FormatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestExtColor(t *testing.T) {
	// Known extensions should return exact colors
	c := ExtColor(".go")
	if c != "#00ADD8" {
		t.Errorf("expected #00ADD8 for .go, got %s", c)
	}
	// Unknown extension should still return a color
	c = ExtColor(".zzzzz")
	if c == "" {
		t.Error("expected non-empty color for unknown extension")
	}
	// Same extension should return same color
	c1 := ExtColor(".zzzzz")
	c2 := ExtColor(".zzzzz")
	if c1 != c2 {
		t.Error("same extension should return same color")
	}
}

func TestBuildTreemapItems(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(tmp+"/a.txt", make([]byte, 100), 0644)
	os.WriteFile(tmp+"/b.go", make([]byte, 50), 0644)

	s := NewScanner(tmp)
	ch := s.Run()
	<-ch

	items := BuildTreemapItems(s.RootNode)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Items should have colors
	for _, item := range items {
		if item.Color == "" {
			t.Error("item should have a color")
		}
	}
}

func TestMain(m *testing.M) {
	// Suppress BubbleTea TUI, just run tests
	os.Exit(m.Run())
}


