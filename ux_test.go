package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFileCountDirCount verifies that computeDirSizes correctly counts
// files and directories recursively, including nested dirs counting themselves.
func TestFileCountDirCount(t *testing.T) {
	tmp := t.TempDir()
	// Structure: root/ -> a.txt, sub/, sub/b.txt, sub/deep/, sub/deep/c.txt
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("aa"), 0644)
	os.Mkdir(filepath.Join(tmp, "sub"), 0755)
	os.WriteFile(filepath.Join(tmp, "sub", "b.txt"), []byte("bb"), 0644)
	os.Mkdir(filepath.Join(tmp, "sub", "deep"), 0755)
	os.WriteFile(filepath.Join(tmp, "sub", "deep", "c.txt"), []byte("cc"), 0644)

	s := NewScanner(tmp)
	<-s.Run()

	// Root: 3 files, 2 dirs (sub + sub/deep)
	if s.RootNode.FileCount != 3 {
		t.Errorf("root FileCount: expected 3, got %d", s.RootNode.FileCount)
	}
	if s.RootNode.DirCount != 2 {
		t.Errorf("root DirCount: expected 2, got %d", s.RootNode.DirCount)
	}

	// sub: 2 files, 1 dir (deep)
	var sub *FileNode
	for _, c := range s.RootNode.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if sub == nil {
		t.Fatal("sub not found")
	}
	if sub.FileCount != 2 {
		t.Errorf("sub FileCount: expected 2, got %d", sub.FileCount)
	}
	if sub.DirCount != 1 {
		t.Errorf("sub DirCount: expected 1, got %d", sub.DirCount)
	}
}

// TestBuildTreemapItemsSorted verifies items are sorted by size descending.
func TestBuildTreemapItemsSorted(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "small.txt"), make([]byte, 10), 0644)
	os.WriteFile(filepath.Join(tmp, "big.txt"), make([]byte, 1000), 0644)
	os.WriteFile(filepath.Join(tmp, "mid.txt"), make([]byte, 100), 0644)

	s := NewScanner(tmp)
	<-s.Run()

	items := BuildTreemapItems(s.RootNode)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Should be sorted: big(1000) > mid(100) > small(10)
	if items[0].Node.Name != "big.txt" {
		t.Errorf("expected big.txt first, got %s", items[0].Node.Name)
	}
	if items[1].Node.Name != "mid.txt" {
		t.Errorf("expected mid.txt second, got %s", items[1].Node.Name)
	}
	if items[2].Node.Name != "small.txt" {
		t.Errorf("expected small.txt third, got %s", items[2].Node.Name)
	}
}

// TestSanitizeName verifies control characters are replaced.
func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"normal.txt", "normal.txt"},
		{"file\nname.txt", "file\\nname.txt"},
		{"file\tname.txt", "file\\tname.txt"},
		{"file\rname.txt", "file\\rname.txt"},
		{"weird\x01char", "weird?char"},
	}
	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.expect {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// TestBreadcrumbPath verifies path shortening.
func TestBreadcrumbPath(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"/home/Kosi/projects", "~/projects"},
		{"/home/Kosi/derKosi/foal", "~/derKosi/foal"},
		{"/home/Kosi", "~/Kosi"},
		{"/var/log", "/var/log"},
		{"/etc", "/etc"},
	}
	for _, tt := range tests {
		got := BreadcrumbPath(tt.input)
		if got != tt.expect {
			t.Errorf("BreadcrumbPath(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// TestSortModes verifies that different sort modes produce different orderings.
func TestSortModes(t *testing.T) {
	tmp := t.TempDir()
	// Create dirs with different sizes and counts
	os.Mkdir(filepath.Join(tmp, "zzz_big"), 0755)
	os.WriteFile(filepath.Join(tmp, "zzz_big", "f.txt"), make([]byte, 1000), 0644)
	os.Mkdir(filepath.Join(tmp, "aaa_small"), 0755)
	os.WriteFile(filepath.Join(tmp, "aaa_small", "f.txt"), make([]byte, 10), 0644)

	s := NewScanner(tmp)
	<-s.Run()
	s.RootNode.Expanded = true

	bySize := FlattenTree(s.RootNode, maxTreeDepth, true, sortBySize)
	// First child should be zzz_big (larger)
	if len(bySize) > 2 && bySize[1].Node.Name != "zzz_big" {
		t.Errorf("sortBySize: expected zzz_big first, got %s", bySize[1].Node.Name)
	}

	byName := FlattenTree(s.RootNode, maxTreeDepth, true, sortByName)
	// First child should be aaa_small (alphabetical)
	if len(byName) > 2 && byName[1].Node.Name != "aaa_small" {
		t.Errorf("sortByName: expected aaa_small first, got %s", byName[1].Node.Name)
	}
}
