package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		follow   bool
		ver      bool
		headless bool
		workers  int
	)
	flag.BoolVar(&follow, "L", false, "follow symlinks into directories (cycle-protected)")
	flag.BoolVar(&headless, "scan", false, "run a headless scan and print a summary, then exit")
	flag.BoolVar(&ver, "version", false, "print version and exit")
	flag.IntVar(&workers, "workers", 0, "max concurrent directory readers (default 64)")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "UnixDirStat %s — TUI disk-usage analyzer\n\n", version)
		fmt.Fprintf(out, "Usage: %s [flags] [path]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if ver {
		fmt.Println("UnixDirStat", version)
		return
	}

	path := "."
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	cfg := ScanConfig{FollowSymlinks: follow, MaxWorkers: workers}

	if headless {
		runHeadless(abs, cfg)
		return
	}

	p := tea.NewProgram(
		NewModel(abs, cfg),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runHeadless(abs string, cfg ScanConfig) {
	s := NewScanner(abs, cfg)
	ch := s.Run()
	start := time.Now()
	<-ch
	elapsed := time.Since(start)

	fmt.Printf("Scan complete in %v\n", elapsed)
	fmt.Printf("  Path:    %s\n", abs)
	fmt.Printf("  Files:   %d\n", s.Stats.FilesScanned.Load())
	fmt.Printf("  Dirs:    %d\n", s.Stats.DirsScanned.Load())
	fmt.Printf("  Links:   %d\n", s.Stats.SymlinksScanned.Load())
	fmt.Printf("  Size:    %s\n", FormatSize(s.RootNode.Size))
	if errs := s.Stats.Errors.Load(); errs > 0 {
		fmt.Printf("  Errors:  %d\n", errs)
		if v := s.Stats.LastError.Load(); v != nil {
			fmt.Printf("           last: %v\n", v)
		}
	}

	s.RootNode.Expanded = true
	exts := GroupByExtension(s.RootNode, true)
	fmt.Printf("\nTop 15 extensions:\n")
	for i, e := range exts {
		if i >= 15 {
			break
		}
		fmt.Printf("  %-10s %8s  %5s  %d files\n", e.Ext, FormatSize(e.Size), FormatPct(e.Size, s.RootNode.Size), e.Count)
	}

	fmt.Printf("\nTop-level contents:\n")
	// Sort by size (desc) via insertion sort — small N at top level.
	children := make([]*FileNode, len(s.RootNode.Children))
	copy(children, s.RootNode.Children)
	for i := 1; i < len(children); i++ {
		for j := i; j > 0 && children[j].Size > children[j-1].Size; j-- {
			children[j], children[j-1] = children[j-1], children[j]
		}
	}
	for _, child := range children {
		icon := "F"
		if child.IsDir {
			icon = "D"
		}
		if child.IsSymlink {
			icon = "L"
		}
		fmt.Printf("  %s %-30s %8s  %5s\n", icon, child.Name, FormatSize(child.Size), FormatPct(child.Size, s.RootNode.Size))
	}

	items := BuildTreemapItems(s.RootNode)
	layout := Squarify(items, Rect{0, 0, 200, 50})
	fmt.Printf("\nTreemap: %d items laid out in 200x50\n", len(layout))
	for _, item := range layout {
		if item.Rect.W > 5 {
			fmt.Printf("  %-30s %dx%d at (%d,%d) %s\n", item.Node.Name, item.Rect.W, item.Rect.H, item.Rect.X, item.Rect.Y, item.Color)
		}
	}
}
