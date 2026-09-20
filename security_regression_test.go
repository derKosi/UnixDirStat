package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateDeleteTargetRefusesSymlinkedAncestor covers the TOCTOU guard
// (BUG-R2-C2-A1-H1 / BUG-R2-C2-A2-H5): a confirmed delete target whose
// ancestor was swapped for a symlink pointing outside the scan root must be
// refused; an untouched in-root target must pass.
func TestValidateDeleteTargetRefusesSymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // stands in for the victim dir (e.g. ~/.config)

	// scan-root/x/y — the node the operator confirmed
	x := filepath.Join(root, "x")
	y := filepath.Join(x, "y")
	if err := os.MkdirAll(y, 0755); err != nil {
		t.Fatal(err)
	}
	// victim/y must exist so the redirect would destroy something
	if err := os.MkdirAll(filepath.Join(outside, "y"), 0755); err != nil {
		t.Fatal(err)
	}

	m := NewModel(root, DefaultScanConfig)
	// validateDeleteTarget only dereferences m.scanner.Root, but use a
	// real completed scanner for realism (Run() signals completion).
	sc := NewScanner(root, DefaultScanConfig)
	<-sc.Run()
	m.scanner = sc

	node := &FileNode{Path: y, IsDir: true}
	if err := m.validateDeleteTarget(node); err != nil {
		t.Fatalf("in-root target must pass, got: %v", err)
	}

	// Attacker swaps ancestor x -> outside (rename-aside + symlink plant)
	if err := os.Rename(x, x+".aside"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, x); err != nil {
		t.Fatal(err)
	}

	err := m.validateDeleteTarget(node)
	if err == nil {
		t.Fatal("swapped ancestor must be refused (delete would redirect outside the scan root)")
	}
}

// TestSanitizeNameExtendedRanges covers the 0x7F DEL and C1 (0x80–0x9F)
// neutralization added for the escape-injection findings: those bytes must
// be replaced, while ordinary text (including non-C1 Unicode) passes through.
func TestSanitizeNameExtendedRanges(t *testing.T) {
	bad := []string{
		"a\x1bb", // ESC (C0, previously handled)
		"a\x7fb", // DEL
		"a\x9bb", // C1 CSI introducer (raw 8-bit)
		"a\x80b", // C1 lower bound
		"a\x9fb", // C1 upper bound
	}
	for _, in := range bad {
		got := SanitizeName(in)
		for _, r := range got {
			if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
				t.Errorf("SanitizeName(%q) = %q still contains control/C1 rune %q", in, got, r)
			}
		}
	}
	good := []string{"clean", "straße", "日本語", "emoji-\U0001F600", "a\tb"}
	for _, in := range good {
		if in == "a\tb" {
			continue // tab is escaped as literal \t by design
		}
		if got := SanitizeName(in); got != in {
			t.Errorf("SanitizeName(%q) = %q must pass through unchanged", in, got)
		}
	}
}
