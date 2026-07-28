package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Panel represents which panel is focused.
type Panel int

const (
	TreePanel Panel = iota
	ExtPanel
	TreemapPanel
)

// FocusState tracks which panel is active.
type FocusState struct {
	ActivePanel Panel
	TreeCursor  int
	ExtCursor   int
}

// ViewDims holds layout dimensions.
type ViewDims struct {
	Width, Height      int
	TreeW, TreeH       int // content area (borders excluded)
	ExtW, ExtH         int
	TreemapW, TreemapH int
}

// ── Header / Footer ───────────────────────────────────────────────

func RenderHeader(path string, stats *ScanStats, width int, sortMode string) string {
	done := stats.Done.Load()
	files := stats.FilesScanned.Load()
	dirs := stats.DirsScanned.Load()
	links := stats.SymlinksScanned.Load()
	size := stats.TotalSize.Load()
	errs := stats.Errors.Load()

	// Breadcrumb-style path: shorten to ~/rest if under /home
	bc := BreadcrumbPath(path)

	var text string
	if done {
		text = fmt.Sprintf(" UnixDirStat  %s  %d files, %d dirs", FormatSize(size), files, dirs)
		if links > 0 {
			text += fmt.Sprintf(", %d links", links)
		}
		text += "  sort:" + sortMode
		text += "  " + bc
		if errs > 0 {
			text += fmt.Sprintf("  \u26a0 %d errors", errs)
		}
	} else {
		current := ""
		if v := stats.CurrentPath.Load(); v != nil {
			current = ShortenPath(v.(string), width-40)
		}
		text = fmt.Sprintf(" Scanning… %s  %d files, %d dirs, %s", current, files, dirs, FormatSize(size))
		if errs > 0 {
			text += fmt.Sprintf("  \u26a0 %d", errs)
		}
	}

	// Truncate / pad to exact width
	runeText := []rune(text)
	if len(runeText) > width {
		runeText = runeText[:width]
	}
	for len(runeText) < width {
		runeText = append(runeText, ' ')
	}

	return lipgloss.NewStyle().
		Background(lipgloss.Color("#16161e")).
		Foreground(lipgloss.Color("#7aa2f7")).
		Bold(true).
		Render(string(runeText))
}

// BreadcrumbPath shortens a path to ~/rest if under /home/user.
func BreadcrumbPath(path string) string {
	home := "/home/"
	if strings.HasPrefix(path, home) {
		rest := path[len(home):]
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			return "~" + rest[slashIdx:]
		}
		return "~/" + rest
	}
	return path
}

func RenderFooter(width int) string {
	keys := " q:quit  Tab:panel  jk:nav  Enter:expand  d:del  .:hidden  r:rescan  s:sort  u:parent  /:path "
	runeKeys := []rune(keys)
	if len(runeKeys) > width {
		runeKeys = runeKeys[:width]
	}
	for len(runeKeys) < width {
		runeKeys = append(runeKeys, ' ')
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#16161e")).
		Foreground(lipgloss.Color("#565f89")).
		Render(string(runeKeys))
}

// ── Tree Panel ────────────────────────────────────────────────────

func RenderTree(nodes []*TreeNode, totalSize int64, cursor int, focused bool, width, height int) string {
	if len(nodes) == 0 {
		return box(" No data yet…", width, height, focused)
	}

	// Scroll window
	maxVisible := height
	start := 0
	if cursor >= maxVisible {
		start = cursor - maxVisible + 5
		if start < 0 {
			start = 0
		}
	}
	end := start + maxVisible
	if end > len(nodes) {
		end = len(nodes)
	}

	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		tn := nodes[i]
		isCursor := i == cursor

		// Tree drawing characters
		connector := tn.TreeConnector()
		expand := tn.ExpandIndicator()
		prefix := tn.Prefix
		if tn.Depth == 0 {
			connector = ""
			prefix = ""
		}

		// Name with color
		name := SanitizeName(tn.Node.Name)
		var nameStyle lipgloss.Style
		switch {
		case tn.Node.IsDir:
			name += "/"
			nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
		case tn.Node.IsSymlink:
			name += "@"
			nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68"))
		default:
			ext := filepath.Ext(name)
			nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ExtColor(ext)))
		}

		// Size bar colored by extension
		barLen := 10
		if totalSize > 0 {
			barLen = int(float64(barLen) * float64(tn.Node.Size) / float64(totalSize))
			if barLen > 10 {
				barLen = 10
			}
			if barLen < 1 && tn.Node.Size > 0 {
				barLen = 1
			}
		}
		barColor := "#565f89"
		if tn.Node.IsDir {
			barColor = "#7aa2f7"
		} else {
			ext := filepath.Ext(tn.Node.Name)
			barColor = ExtColor(ext)
		}
		barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(barColor))
		bar := barStyle.Render(strings.Repeat("█", barLen)) + strings.Repeat("░", 10-barLen)

		// Count info for directories
		countStr := ""
		if tn.Node.IsDir {
			countStr = fmt.Sprintf("%df %dd", tn.Node.FileCount, tn.Node.DirCount)
		}

		pct := FormatPct(tn.Node.Size, totalSize)
		sizeStr := FormatSize(tn.Node.Size)
		rightPart := fmt.Sprintf("%8s %5s %s", sizeStr, pct, bar)
		if countStr != "" {
			rightPart = fmt.Sprintf("%8s %5s %-10s %s", sizeStr, pct, countStr, bar)
		}
		leftPart := fmt.Sprintf("%s%s%s", prefix, connector, expand)

		avail := width - runeWidth(leftPart) - runeWidth(rightPart) - 3
		if avail < 4 {
			avail = 4
		}
		nameRunes := []rune(name)
		if len(nameRunes) > avail {
			nameRunes = append(nameRunes[:avail-1], '…')
			name = string(nameRunes)
		}

		nameColored := nameStyle.Render(name)
		line := fmt.Sprintf("%s %s  %s", leftPart, padRight(nameColored, avail), rightPart)

		if isCursor && focused {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#283457")).
				Foreground(lipgloss.Color("#c0caf5")).
				Width(width).
				Render(line)
		}
		lines = append(lines, line)
	}

	// Pad
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}

	return box(strings.Join(lines, "\n"), width, height, focused)
}

// ── Extension Panel ───────────────────────────────────────────────

func RenderExtensions(exts []*ExtGroup, cursor int, focused bool, totalSize int64, width, height int) string {
	if len(exts) == 0 {
		return box(" No data yet…", width, height, focused)
	}

	start := 0
	if cursor >= height {
		start = cursor - height + 5
		if start < 0 {
			start = 0
		}
	}
	end := start + height
	if end > len(exts) {
		end = len(exts)
	}

	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		ext := exts[i]
		isCursor := i == cursor

		pct := FormatPct(ext.Size, totalSize)
		barLen := 20
		if totalSize > 0 {
			barLen = int(float64(barLen) * float64(ext.Size) / float64(totalSize))
			if barLen > 20 {
				barLen = 20
			}
			if barLen < 1 && ext.Size > 0 {
				barLen = 1
			}
		}

		colorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ext.Color)).Bold(true)
		block := colorStyle.Render(strings.Repeat("█", barLen))
		empty := lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1b26")).Render(strings.Repeat("░", 20-barLen))

		extLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(ext.Color)).Bold(true).Render(fmt.Sprintf("%-8s", ext.Ext))
		line := fmt.Sprintf("%s %3d  %8s %5s %s%s", extLabel, ext.Count, FormatSize(ext.Size), pct, block, empty)

		if isCursor && focused {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#283457")).
				Foreground(lipgloss.Color("#c0caf5")).
				Width(width).
				Render(line)
		}
		lines = append(lines, line)
	}

	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}

	return box(strings.Join(lines, "\n"), width, height, focused)
}

// ── Treemap Panel ─────────────────────────────────────────────────

func RenderTreemap(items []TreemapItem, focused bool, width, height int) string {
	if len(items) == 0 {
		return box(" No data yet…", width, height, focused)
	}

	// Grid-based rendering: each cell holds an ANSI-styled string
	grid := make([][]string, height)
	for y := range grid {
		grid[y] = make([]string, width)
		for x := range grid[y] {
			grid[y][x] = " "
		}
	}

	for i := range items {
		item := &items[i]
		if item.Rect.W <= 0 || item.Rect.H <= 0 {
			continue
		}
		if item.Node == nil {
			continue
		}

		bg := lipgloss.Color(item.Color)
		bgStyle := lipgloss.NewStyle().Background(bg)

		// Fill the rectangle with solid background color.
		// One lipgloss.Render per row instead of per cell.
		endX := item.Rect.X + item.Rect.W
		if endX > width {
			endX = width
		}
		fillWidth := endX - item.Rect.X
		if fillWidth > 0 {
			fillStr := bgStyle.Render(strings.Repeat(" ", fillWidth))
			fillRunes := []rune(fillStr)
			for y := item.Rect.Y; y < item.Rect.Y+item.Rect.H && y < height; y++ {
				if y < 0 {
					continue
				}
				for i, r := range fillRunes {
					px := item.Rect.X + i
					if px >= 0 && px < width {
						grid[y][px] = string(r)
					}
				}
			}
		}

		// Render label on top if big enough
		if item.Rect.W >= 6 && item.Rect.H >= 2 {
			label := item.Node.Name
			maxLabel := item.Rect.W - 2
			runeLabel := []rune(label)
			if len(runeLabel) > maxLabel {
				runeLabel = append(runeLabel[:maxLabel-1], '…')
			}
			labelStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#ffffff")).Bold(true)
			for ci, ch := range runeLabel {
				px := item.Rect.X + 1 + ci
				py := item.Rect.Y
				if px < width && py < height && px >= 0 && py >= 0 {
					grid[py][px] = labelStyle.Render(string(ch))
				}
			}
			// Second line: size
			if item.Rect.H >= 3 {
				sizeLabel := FormatSize(item.Size)
				sizeRunes := []rune(sizeLabel)
				if len(sizeRunes) > maxLabel {
					sizeRunes = sizeRunes[:maxLabel]
				}
				sizeStyle := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#c0caf5"))
				for ci, ch := range sizeRunes {
					px := item.Rect.X + 1 + ci
					py := item.Rect.Y + 1
					if px < width && py < height && px >= 0 && py >= 0 {
						grid[py][px] = sizeStyle.Render(string(ch))
					}
				}
			}
		}
	}

	var sb strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sb.WriteString(grid[y][x])
		}
		if y < height-1 {
			sb.WriteString("\n")
		}
	}

	return box(sb.String(), width, height, focused)
}

// ── Helpers ───────────────────────────────────────────────────────

// box wraps content in a bordered box that fits exactly width×height.
func box(content string, width, height int, focused bool) string {
	borderFg := lipgloss.Color("#3b4261")
	if focused {
		borderFg = lipgloss.Color("#7aa2f7")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderFg).
		Width(width - 2).   // -2 for left+right border chars
		Height(height - 2). // -2 for top+bottom border lines
		Render(content)
}

// runeWidth returns the display width of a string (ignoring ANSI escapes roughly).
func runeWidth(s string) int {
	// Strip ANSI escape sequences for measurement
	n := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		n++
	}
	return n
}

// padRight pads a string with spaces to target display width.
func padRight(s string, targetWidth int) string {
	currentWidth := runeWidth(s)
	if currentWidth >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-currentWidth)
}

// darken reduces a hex color's brightness by a factor (0-1).
func darken(hex string, factor float64) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "#000000"
	}
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	r = int(float64(r) * (1 - factor))
	g = int(float64(g) * (1 - factor))
	b = int(float64(b) * (1 - factor))
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
