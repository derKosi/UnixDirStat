package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtGroup aggregates files by extension.
type ExtGroup struct {
	Ext   string
	Color string
	Size  int64
	Count int
}

// GroupByExtension walks the tree and groups files by extension.
func GroupByExtension(root *FileNode) []*ExtGroup {
	groups := map[string]*ExtGroup{}
	walkExtensions(root, groups)

	result := make([]*ExtGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, g)
	}
	// Sort by size descending
	sortBySize(result)
	return result
}

func walkExtensions(node *FileNode, groups map[string]*ExtGroup) {
	if node == nil {
		return
	}
	if !node.IsDir {
		ext := strings.ToLower(filepath.Ext(node.Name))
		if ext == "" {
			ext = "(none)"
		}
		color := ExtColor(ext)
		if g, ok := groups[ext]; ok {
			g.Size += node.Size
			g.Count++
		} else {
			groups[ext] = &ExtGroup{Ext: ext, Color: color, Size: node.Size, Count: 1}
		}
	}
	for _, child := range node.Children {
		walkExtensions(child, groups)
	}
}

func sortBySize(groups []*ExtGroup) {
	// Simple insertion sort (fine for typical extension counts ~50-200)
	for i := 1; i < len(groups); i++ {
		j := i
		for j > 0 && groups[j].Size > groups[j-1].Size {
			groups[j], groups[j-1] = groups[j-1], groups[j]
			j--
		}
	}
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// FormatPct returns a percentage string.
func FormatPct(part, total int64) string {
	if total == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// IsHidden returns true if the file name starts with '.'.
func IsHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

// CanRead checks if we can read a directory.
func CanRead(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
