package main

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const tickMS = 100

// Wrapper to satisfy tea.Cmd for polling
func pollTick() tea.Cmd {
	return tea.Tick(time.Duration(tickMS)*time.Millisecond, func(t time.Time) tea.Msg {
		return scanUpdateMsg{}
	})
}

func main() {
	path := "."
	headless := false
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--scan" || os.Args[i] == "-scan" {
			headless = true
			continue
		}
		path = os.Args[i]
	}

	abs, err := filepathAbs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if headless {
		runHeadless(abs)
		return
	}

	p := tea.NewProgram(
		NewModel(abs),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runHeadless(abs string) {
	s := NewScanner(abs)
	ch := s.Run()
	start := time.Now()
	<-ch
	elapsed := time.Since(start)

	fmt.Printf("Scan complete in %v\n", elapsed)
	fmt.Printf("  Path:  %s\n", abs)
	fmt.Printf("  Files: %d\n", s.Stats.FilesScanned.Load())
	fmt.Printf("  Dirs:  %d\n", s.Stats.DirsScanned.Load())
	fmt.Printf("  Size:  %s\n", FormatSize(s.RootNode.Size))

	s.RootNode.Expanded = true
	exts := GroupByExtension(s.RootNode)
	fmt.Printf("\nTop 15 extensions:\n")
	for i, e := range exts {
		if i >= 15 {
			break
		}
		fmt.Printf("  %-10s %8s  %5s  %d files\n", e.Ext, FormatSize(e.Size), FormatPct(e.Size, s.RootNode.Size), e.Count)
	}

	fmt.Printf("\nTop-level contents:\n")
	// Sort by size
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

func filepathAbs(path string) (string, error) {
	if path == "" {
		path = "."
	}
	// Simple absolute path resolution
	if path[0] != '/' {
		cwd, err := os.Getwd()
		if err != nil {
			return path, err
		}
		path = cwd + "/" + path
	}
	return path, nil
}
