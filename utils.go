package main

import (
	"fmt"
	"path/filepath"
	"sort"
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
// When showHidden is false, dotfiles and dot-directories are excluded.
func GroupByExtension(root *FileNode, showHidden bool) []*ExtGroup {
	groups := map[string]*ExtGroup{}
	walkExtensions(root, groups, showHidden)

	result := make([]*ExtGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, g)
	}
	// Sort by size descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Size > result[j].Size
	})
	return result
}

func walkExtensions(node *FileNode, groups map[string]*ExtGroup, showHidden bool) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		if !showHidden && IsHidden(child.Name) {
			continue
		}
		if !child.IsDir {
			ext := strings.ToLower(filepath.Ext(child.Name))
			if ext == "" {
				ext = "(none)"
			}
			color := ExtColor(ext)
			if g, ok := groups[ext]; ok {
				g.Size += child.Size
				g.Count++
			} else {
				groups[ext] = &ExtGroup{Ext: ext, Color: color, Size: child.Size, Count: 1}
			}
			continue
		}
		walkExtensions(child, groups, showHidden)
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

// SanitizeName replaces control characters (newlines, tabs, etc.) in
// filenames with a literal escape sequence to prevent TUI layout corruption.
func SanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r == '\n':
			sb.WriteString("\\n")
		case r == '\r':
			sb.WriteString("\\r")
		case r == '	':
		sb.WriteString("\\t")
		case r < 0x20:
			sb.WriteString("?")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
