package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// ── padRight: regression test after runeWidth → lipgloss.Width switch ──

func TestPadRight(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		target   int
		wantLen  int
		wantPad  bool // whether trailing spaces were added
	}{
		{"plain short", "hi", 10, 10, true},
		{"exact width", "hello", 5, 5, false},
		{"too long (no truncation)", "hello world", 3, 11, false},
		{"empty string", "", 5, 5, true},
		{"zero target", "x", 0, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padRight(tt.input, tt.target)
			if lipgloss.Width(got) != tt.wantLen {
				t.Errorf("padRight(%q,%d): display width = %d, want %d",
					tt.input, tt.target, lipgloss.Width(got), tt.wantLen)
			}
			hasTrailing := strings.HasSuffix(got, " ") && got != ""
			if tt.input != "" && hasTrailing != tt.wantPad {
				t.Errorf("padRight(%q,%d): trailing-space=%v, want %v",
					tt.input, tt.target, hasTrailing, tt.wantPad)
			}
		})
	}
}

// padRight must correctly measure ANSI-styled strings. The old runeWidth()
// used a hand-rolled heuristic; lipgloss.Width handles escapes robustly.
// This test guarantees the refactor didn't regress ANSI measurement.
func TestPadRightWithANSI(t *testing.T) {
	// "\x1b[31mhi\x1b[0m" has display width 2 but byte length 13.
	styled := "\x1b[31mhi\x1b[0m"
	padded := padRight(styled, 10)
	if w := lipgloss.Width(padded); w != 10 {
		t.Errorf("ANSI pad: display width = %d, want 10 (byte len=%d)",
			w, len(padded))
	}
}

// ── ShortenPath ──────────────────────────────────────────────────────

func TestShortenPath(t *testing.T) {
	// ShortenPath only activates when len(path) > maxLen; short paths
	// pass through unchanged regardless of /home/ prefix.
	short := "/short/path"
	if got := ShortenPath(short, 100); got != short {
		t.Errorf("short path: got %q, want %q", got, short)
	}

	// Long path under /home/user → ~/rest shortening if it fits,
	// otherwise falls through to middle truncation.
	long := "/home/user/" + strings.Repeat("subdir/", 10) + "file.txt"
	got := ShortenPath(long, 40)
	if len(got) > 40 {
		t.Errorf("result too long: %d > 40 (%q)", len(got), got)
	}
	// Long enough to trigger the len>maxLen guard, but ~/rest fits.
	// /home/user/deep/path/structure/file.txt = 41 bytes
	// ~/deep/path/structure/file.txt = 32 bytes
	// With maxLen=35: guard triggers (41>35), ~/rest fits (32≤35).
	homeLong := "/home/user/deep/path/structure/file.txt"
	got = ShortenPath(homeLong, 35)
	if !strings.HasPrefix(got, "~/") {
		t.Errorf("long /home path: expected ~/ prefix, got %q", got)
	}

	// Long path NOT under /home → middle truncation.
	other := "/var/log/" + strings.Repeat("x", 60) + "/app.log"
	got = ShortenPath(other, 30)
	if !strings.Contains(got, "…") {
		t.Errorf("long non-/home path: expected middle ellipsis, got %q", got)
	}
	// Result: half + "…" + half bytes where half = (maxLen-3)/2.
	// half=13 → 13 + 3 + 13 = 29 bytes.
	if len(got) > 29 {
		t.Errorf("result too long: %d > 29 (%q)", len(got), got)
	}
}

// ── TreeConnector / ExpandIndicator ──────────────────────────────────

func TestTreeConnector(t *testing.T) {
	root := &TreeNode{Depth: 0}
	if got := root.TreeConnector(); got != "" {
		t.Errorf("depth-0 connector = %q, want empty", got)
	}
	last := &TreeNode{Depth: 1, IsLast: true}
	if got := last.TreeConnector(); got != "└──" {
		t.Errorf("last child connector = %q, want └──", got)
	}
	mid := &TreeNode{Depth: 1, IsLast: false}
	if got := mid.TreeConnector(); got != "├──" {
		t.Errorf("mid child connector = %q, want ├──", got)
	}
}

func TestExpandIndicator(t *testing.T) {
	fileNode := &TreeNode{Node: &FileNode{IsDir: false}}
	if got := fileNode.ExpandIndicator(); got != " " {
		t.Errorf("file indicator = %q, want ' '", got)
	}
	collapsed := &TreeNode{Node: &FileNode{IsDir: true, Expanded: false}}
	if got := collapsed.ExpandIndicator(); got != "▸" {
		t.Errorf("collapsed dir indicator = %q, want ▸", got)
	}
	expanded := &TreeNode{Node: &FileNode{IsDir: true, Expanded: true}}
	if got := expanded.ExpandIndicator(); got != "▾" {
		t.Errorf("expanded dir indicator = %q, want ▾", got)
	}
}

// ── FormatPct ────────────────────────────────────────────────────────

func TestFormatPct(t *testing.T) {
	tests := []struct {
		part, total int64
		want        string
	}{
		{0, 0, "0.0%"},
		{50, 0, "0.0%"}, // avoid div-by-zero
		{50, 100, "50.0%"},
		{1, 3, "33.3%"},
		{2, 3, "66.7%"},
		{0, 100, "0.0%"},
		{100, 100, "100.0%"},
	}
	for _, tt := range tests {
		got := FormatPct(tt.part, tt.total)
		if got != tt.want {
			t.Errorf("FormatPct(%d,%d) = %q, want %q",
				tt.part, tt.total, got, tt.want)
		}
	}
}

// ── Treemap edge cases ───────────────────────────────────────────────

// TestSquarifyZeroSize: items with size 0 must be filtered out, not laid out.
func TestSquarifyZeroSize(t *testing.T) {
	items := []TreemapItem{
		{Size: 0, Color: "#ff0000", Node: &FileNode{Name: "zero"}},
		{Size: 0, Color: "#00ff00", Node: &FileNode{Name: "zero2"}},
	}
	result := Squarify(items, Rect{0, 0, 80, 40})
	if len(result) != 0 {
		t.Errorf("all-zero-size items: expected 0 laid out, got %d", len(result))
	}
}

// TestSquarifyAllZeroBounds: bounds with 0 area → no layout.
func TestSquarifyZeroBounds(t *testing.T) {
	items := []TreemapItem{
		{Size: 100, Color: "#ff0000"},
	}
	for _, bounds := range []Rect{{0, 0, 0, 40}, {0, 0, 80, 0}, {0, 0, 0, 0}} {
		result := Squarify(items, bounds)
		for _, it := range result {
			if it.Rect.W > 0 && it.Rect.H > 0 {
				t.Errorf("zero-bounds %v: item got non-empty rect %+v", bounds, it.Rect)
			}
		}
	}
}

// TestSquarifyTwoItemsSameSize verifies layout fills area exactly for equal items.
func TestSquarifyTwoItemsSameSize(t *testing.T) {
	items := []TreemapItem{
		{Size: 50, Color: "#ff0000", Node: &FileNode{Name: "a"}},
		{Size: 50, Color: "#00ff00", Node: &FileNode{Name: "b"}},
	}
	bounds := Rect{0, 0, 100, 40}
	result := Squarify(items, bounds)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	totalArea := 0
	for _, it := range result {
		totalArea += it.Rect.W * it.Rect.H
	}
	wantArea := bounds.W * bounds.H
	if totalArea != wantArea {
		t.Errorf("area fill: got %d, want %d", totalArea, wantArea)
	}
}

// TestSquarifyRectsDoNotOverlap verifies laid-out rects are non-overlapping.
func TestSquarifyRectsDoNotOverlap(t *testing.T) {
	items := []TreemapItem{
		{Size: 60, Color: "#ff0000", Node: &FileNode{Name: "a"}},
		{Size: 30, Color: "#00ff00", Node: &FileNode{Name: "b"}},
		{Size: 20, Color: "#0000ff", Node: &FileNode{Name: "c"}},
		{Size: 10, Color: "#ffff00", Node: &FileNode{Name: "d"}},
	}
	bounds := Rect{0, 0, 100, 50}
	result := Squarify(items, bounds)

	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			a, b := result[i].Rect, result[j].Rect
			overlap := a.X < b.X+b.W && a.X+a.W > b.X &&
				a.Y < b.Y+b.H && a.Y+a.H > b.Y
			if overlap {
				t.Errorf("rects overlap: %d=%+v  %d=%+v", i, a, j, b)
			}
		}
	}
}

// ── Scanner edge cases ───────────────────────────────────────────────

// TestScannerPermissionError verifies that an unreadable subdir is recorded
// as an error, not a crash. We skip if we can't drop permissions (root)
// or on Windows where POSIX chmod has no effect.
func TestScannerPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission test meaningless")
	}
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0000 has no effect on Windows (ACL-based permissions)")
	}
	tmp := t.TempDir()
	// Create a dir, put a file in it, then remove read+execute permission.
	denied := filepath.Join(tmp, "denied")
	os.Mkdir(denied, 0755)
	os.WriteFile(filepath.Join(denied, "f.txt"), []byte("x"), 0644)
	os.Chmod(denied, 0000)
	defer os.Chmod(denied, 0755) // restore so TempDir cleanup works

	s := NewScanner(tmp)
	<-s.Run()

	if got := s.Stats.Errors.Load(); got < 1 {
		t.Errorf("expected ≥1 scan error from denied dir, got %d", got)
	}
	if v := s.Stats.LastError.Load(); v == nil {
		t.Error("LastError should be set on scan error")
	}
}

// TestScanConfigResolution verifies resolveConfig defaults and overrides.
func TestScanConfigResolution(t *testing.T) {
	// No config → defaults.
	cfg := resolveConfig()
	if cfg.MaxWorkers != 64 {
		t.Errorf("default MaxWorkers = %d, want 64", cfg.MaxWorkers)
	}
	if cfg.FollowSymlinks {
		t.Error("default FollowSymlinks should be false")
	}
	// Explicit override.
	cfg = resolveConfig(ScanConfig{FollowSymlinks: true, MaxWorkers: 8})
	if cfg.MaxWorkers != 8 || !cfg.FollowSymlinks {
		t.Errorf("override not respected: %+v", cfg)
	}
	// Zero workers → default.
	cfg = resolveConfig(ScanConfig{MaxWorkers: 0})
	if cfg.MaxWorkers != 64 {
		t.Errorf("zero workers should fall back to 64, got %d", cfg.MaxWorkers)
	}
	// Negative workers → default.
	cfg = resolveConfig(ScanConfig{MaxWorkers: -1})
	if cfg.MaxWorkers != 64 {
		t.Errorf("negative workers should fall back to 64, got %d", cfg.MaxWorkers)
	}
}

// TestDirColorDeterminism verifies the same directory name always maps
// to the same color, and different names generally map to different colors.
func TestDirColorDeterminism(t *testing.T) {
	c1 := dirColor("src")
	c2 := dirColor("src")
	if c1 != c2 {
		t.Errorf("dirColor not deterministic: %q vs %q for 'src'", c1, c2)
	}
	// 'src' and 'docs' should almost certainly differ (12-color palette).
	if dirColor("src") == dirColor("docs") {
		t.Log("note: src and docs happen to collide on same palette color")
	}
	// Must be a valid palette member.
	found := false
	for _, c := range dirPalette {
		if c == c1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dirColor returned %q, not in dirPalette", c1)
	}
}

// ── Headless mode integration (binary required) ─────────────────────

// TestHeadlessScanOutput runs the built binary with -scan on a temp dir
// and verifies the expected output format. Skips if the binary is absent
// or if GOOS doesn't match the build target.
func TestHeadlessScanOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("headless integration test is linux-only")
	}
	bin := buildBin(t)

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.txt"), make([]byte, 100), 0644)
	os.WriteFile(filepath.Join(tmp, "b.go"), make([]byte, 50), 0644)

	cmd := exec.Command(bin, "-scan", tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless scan failed: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{
		"Scan complete",
		"Path:",
		"Files:",
		"Dirs:",
		"Size:",
		"Top 15 extensions",
		"a.txt", // top-level content listing
	} {
		if !strings.Contains(s, want) {
			t.Errorf("headless output missing %q\n--- output ---\n%s", want, s)
		}
	}
}

// TestHeadlessNonexistentPath verifies graceful handling of a bad path.
func TestHeadlessNonexistentPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("headless integration test is linux-only")
	}
	bin := buildBin(t)

	cmd := exec.Command(bin, "-scan", "/nonexistent/path/that/should/not/exist")
	out, err := cmd.CombinedOutput()
	// Headless mode logs the error and continues (exit 0), not a crash.
	if err != nil {
		t.Fatalf("headless scan of bad path should not error, got: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Errors:") {
		t.Errorf("expected 'Errors:' in output for bad path, got:\n%s", out)
	}
}

// TestHeadlessVersionFlag verifies -version exits cleanly.
func TestHeadlessVersionFlag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("headless integration test is linux-only")
	}
	bin := buildBin(t)

	cmd := exec.Command(bin, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("-version should exit cleanly, got: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "UnixDirStat ") {
		t.Errorf("-version output unexpected: %q", out)
	}
}

// ── BreadcrumbPath: extra coverage for non-/home paths ───────────────

func TestBreadcrumbPathEdges(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/home/Kosi", "~/Kosi"}, // user dir only, no subdir
		{"/home/", "~/"},         // edge: exactly /home/ → ~/
		{"/tmp", "/tmp"},         // not under /home
		{"", ""},                 // empty
		{"/home", "/home"},       // exactly /home, no trailing slash
	}
	for _, tt := range tests {
		got := BreadcrumbPath(tt.input)
		if got != tt.want {
			t.Errorf("BreadcrumbPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────

// buildBin compiles the binary to a temp path and returns its path.
// The test package t is used for cleanup and fatal-on-build-failure.
func buildBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "uds-test")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}
