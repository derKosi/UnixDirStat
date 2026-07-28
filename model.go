package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// scanUpdateMsg is sent periodically during scanning.
type scanUpdateMsg struct{}

// rebuildThrottle limits how often the (expensive) tree/extensions/treemap
// rebuild runs during a scan. The header counter stays live because it
// reads atomic stats directly on every render.
const rebuildThrottle = 400 * time.Millisecond

type modalKind int

const (
	modalNone modalKind = iota
	modalConfirmDelete
	modalMessage
)

// sortMode controls how tree children are sorted.
type sortMode int

const (
	sortBySize sortMode = iota
	sortByName
	sortByCount
)

// Model is the BubbleTea model for UnixDirStat.
type Model struct {
	scanner    *Scanner
	path       string
	cfg        ScanConfig
	exts       []*ExtGroup
	treemap    []TreemapItem
	flatTree   []*TreeNode
	focus      FocusState
	input      textinput.Model
	inputMode  bool
	showHidden bool
	ready      bool
	viewDims   ViewDims
	sortMode   sortMode

	lastRebuild time.Time

	// modal state
	modal     modalKind
	modalNode *FileNode
	modalMsg  string
}

func NewModel(path string, cfg ScanConfig) Model {
	ti := textinput.New()
	ti.Placeholder = "/path/to/scan"
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 50

	return Model{
		path:       path,
		cfg:        cfg,
		scanner:    NewScanner(path, cfg),
		input:      ti,
		showHidden: true,
		focus:      FocusState{ActivePanel: TreePanel},
	}
}

func (m Model) Init() tea.Cmd {
	ch := m.scanner.Run()
	return tea.Batch(
		func() tea.Msg {
			<-ch
			return scanUpdateMsg{}
		},
		m.pollUpdates(),
		textinput.Blink,
	)
}

func (m Model) pollUpdates() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return scanUpdateMsg{}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewDims.Width = msg.Width
		m.viewDims.Height = msg.Height
		m.recalcLayout()
		m.ready = true
		m.rebuildViews()
		return m, nil

	case tea.KeyMsg:
		if m.modal != modalNone {
			return m.handleModal(msg)
		}
		if m.inputMode {
			return m.handleInputMode(msg)
		}
		return m.handleKeyMsg(msg)

	case scanUpdateMsg:
		if m.scanner != nil {
			now := time.Now()
			if m.scanner.Stats.Done.Load() || now.Sub(m.lastRebuild) >= rebuildThrottle {
				m.rebuildViews()
				m.lastRebuild = now
			}
			if m.scanner.Stats.Done.Load() {
				return m, nil
			}
			return m, m.pollUpdates()
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab":
		m.focus.ActivePanel = (m.focus.ActivePanel + 1) % 3

	case "r":
		return m, m.rescan()

	case "/":
		m.inputMode = true
		m.input.SetValue(m.path)
		return m, textinput.Blink

	case ".":
		// Toggle visibility of dotfiles in the tree/extensions views.
		m.showHidden = !m.showHidden
		m.rebuildViews()

	case "d", "D":
		return m.startDelete(), nil

	case "s":
		// Cycle sort mode: size -> name -> count -> size
		m.sortMode = (m.sortMode + 1) % 3
		m.rebuildViews()

	case "u":
		// Navigate to parent directory in the tree (jump cursor to parent)
		m.jumpToParent()

	case "up", "k":
		m.moveCursor(-1)

	case "down", "j":
		m.moveCursor(1)

	case "enter", " ":
		m.handleEnter()
	}

	return m, nil
}

func (m *Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		path := strings.TrimSpace(m.input.Value())
		if path != "" {
			m.path = path
			m.inputMode = false
			return m, m.rescan()
		}
		m.inputMode = false
		return m, nil

	case "esc":
		m.inputMode = false
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleModal routes keys while a modal is open.
func (m *Model) handleModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalConfirmDelete:
		switch msg.String() {
		case "y", "Y", "enter":
			node := m.modalNode
			m.modal = modalNone
			m.modalNode = nil
			if node == nil {
				return m, nil
			}
			var err error
			if node.IsDir {
				err = os.RemoveAll(node.Path)
			} else {
				err = os.Remove(node.Path)
			}
			if err != nil {
				m.modal = modalMessage
				m.modalMsg = "Delete failed: " + err.Error()
				return m, nil
			}
			return m, m.rescan()

		case "n", "N", "esc":
			m.modal = modalNone
			m.modalNode = nil
			return m, nil
		}
		return m, nil

	case modalMessage:
		// Any key dismisses the message.
		m.modal = modalNone
		m.modalMsg = ""
		return m, nil
	}
	return m, nil
}

// startDelete opens the confirm modal for the node under the tree cursor.
// Refuses to delete the scan root.
func (m *Model) startDelete() tea.Model {
	if m.focus.ActivePanel != TreePanel || m.scanner == nil || m.scanner.RootNode == nil {
		return m
	}
	nodes := m.flatTree
	if m.focus.TreeCursor < 0 || m.focus.TreeCursor >= len(nodes) {
		return m
	}
	node := nodes[m.focus.TreeCursor].Node
	if node == m.scanner.RootNode {
		m.modal = modalMessage
		m.modalMsg = "Refusing to delete the scan root: " + node.Path
		return m
	}
	m.modal = modalConfirmDelete
	m.modalNode = node
	return m
}

// rescan re-creates the scanner with the current path+config and clears
// all derived view state. Cursor positions are reset to 0 so they don't
// point at stale indices after the tree changes.
func (m *Model) rescan() tea.Cmd {
	m.scanner = NewScanner(m.path, m.cfg)
	m.exts = nil
	m.treemap = nil
	m.flatTree = nil
	m.focus.TreeCursor = 0
	m.focus.ExtCursor = 0
	ch := m.scanner.Run()
	return tea.Batch(
		func() tea.Msg { <-ch; return scanUpdateMsg{} },
		m.pollUpdates(),
	)
}

func (m *Model) moveCursor(delta int) {
	switch m.focus.ActivePanel {
	case TreePanel:
		nodes := m.flatTree
		m.focus.TreeCursor += delta
		if m.focus.TreeCursor < 0 {
			m.focus.TreeCursor = 0
		}
		if nodes != nil && m.focus.TreeCursor >= len(nodes) {
			m.focus.TreeCursor = len(nodes) - 1
		}
		m.rebuildTreemapForSelection()

	case ExtPanel:
		m.focus.ExtCursor += delta
		if m.focus.ExtCursor < 0 {
			m.focus.ExtCursor = 0
		}
		if m.exts != nil && m.focus.ExtCursor >= len(m.exts) {
			m.focus.ExtCursor = len(m.exts) - 1
		}
	}
}

func (m *Model) handleEnter() {
	if m.focus.ActivePanel != TreePanel || m.scanner == nil || m.scanner.RootNode == nil {
		return
	}
	nodes := m.flatTree
	if m.focus.TreeCursor < 0 || m.focus.TreeCursor >= len(nodes) {
		return
	}
	node := nodes[m.focus.TreeCursor].Node
	if node.IsDir {
		node.Expanded = !node.Expanded
		m.rebuildViews()
	}
}

// jumpToParent moves the cursor to the parent directory of the
// currently selected node, collapsing the current dir if expanded.
func (m *Model) jumpToParent() {
	if m.focus.ActivePanel != TreePanel || len(m.flatTree) == 0 {
		return
	}
	if m.focus.TreeCursor < 0 || m.focus.TreeCursor >= len(m.flatTree) {
		return
	}
	current := m.flatTree[m.focus.TreeCursor].Node
	if current.Parent == nil {
		return
	}
	// Collapse current dir if it's a dir, then jump to parent
	if current.IsDir && current.Expanded {
		current.Expanded = false
		m.rebuildViews()
	}
	// Find parent in the new flatTree
	for i, tn := range m.flatTree {
		if tn.Node == current.Parent {
			m.focus.TreeCursor = i
			m.rebuildTreemapForSelection()
			return
		}
	}
}

// sortLabel returns a human-readable label for the current sort mode.
func (m *Model) sortLabel() string {
	switch m.sortMode {
	case sortByName:
		return "name"
	case sortByCount:
		return "count"
	default:
		return "size"
	}
}

// rebuildTreemapForSelection rebuilds the treemap to show the contents
// of the currently selected directory in the tree.
func (m *Model) rebuildTreemapForSelection() {
	if m.scanner == nil || m.scanner.RootNode == nil {
		return
	}
	nodes := m.flatTree
	if len(nodes) == 0 || m.focus.TreeCursor < 0 || m.focus.TreeCursor >= len(nodes) {
		return
	}
	selected := nodes[m.focus.TreeCursor].Node

	// If the selected node is a directory, show its children; otherwise
	// show the parent's children (highlighting context for the file).
	target := selected
	if !selected.IsDir && selected.Parent != nil {
		target = selected.Parent
	}

	tw := m.viewDims.TreemapW - 4 // account for borders
	th := m.viewDims.TreemapH - 2
	if tw < 10 || th < 5 {
		return
	}

	m.treemap = Squarify(BuildTreemapItems(target), Rect{0, 0, tw, th})
}

func (m *Model) recalcLayout() {
	w, h := m.viewDims.Width, m.viewDims.Height
	// Minimum terminal size; below this we just render what we can.
	m.viewDims.TreeW = minInt(w-2, 40)
	m.viewDims.TreeH = minInt(h-4, 5)
	m.viewDims.ExtW = minInt(w-2, 20)
	m.viewDims.ExtH = minInt(h-4, 5)
	m.viewDims.TreemapW = w
	m.viewDims.TreemapH = minInt(h-4, 5)
	if w < 40 || h < 15 {
		return
	}

	// Layout: header(1) + topPanels + treemap + footer(1) = h
	// topPanels = 40% of usable, treemap = 60%
	borderOverhead := 2 // top+bottom border lines per panel

	usableH := h - 2 // header + footer
	topH := usableH * 2 / 5

	// Width: tree 60%, extensions 40%
	treeW := w * 3 / 5
	extW := w - treeW

	m.viewDims.TreeW = treeW
	m.viewDims.TreeH = topH - borderOverhead
	m.viewDims.ExtW = extW
	m.viewDims.ExtH = topH - borderOverhead
	m.viewDims.TreemapW = w
	m.viewDims.TreemapH = (usableH - topH) - borderOverhead
}

// rebuildViews recomputes all derived view state: the flat (visible) tree,
// the extension groups, and the treemap. Cursor positions are clamped.
func (m *Model) rebuildViews() {
	if m.scanner == nil || m.scanner.RootNode == nil {
		return
	}
	if !m.scanner.RootNode.Expanded {
		m.scanner.RootNode.Expanded = true
	}

	m.flatTree = FlattenTree(m.scanner.RootNode, maxTreeDepth, m.showHidden, m.sortMode)
	m.exts = GroupByExtension(m.scanner.RootNode, m.showHidden)

	if nodes := m.flatTree; nodes != nil {
		if m.focus.TreeCursor >= len(nodes) {
			m.focus.TreeCursor = len(nodes) - 1
		}
		if m.focus.TreeCursor < 0 {
			m.focus.TreeCursor = 0
		}
	}
	if m.exts != nil {
		if m.focus.ExtCursor >= len(m.exts) {
			m.focus.ExtCursor = len(m.exts) - 1
		}
		if m.focus.ExtCursor < 0 {
			m.focus.ExtCursor = 0
		}
	}

	m.rebuildTreemapForSelection()
}

func (m Model) View() string {
	if !m.ready {
		return "Loading…"
	}
	if m.modal != modalNone {
		return m.renderModalScreen()
	}
	if m.inputMode {
		return m.renderInputScreen()
	}
	return m.renderMain()
}

func (m Model) renderMain() string {
	var sb strings.Builder
	w := m.viewDims.Width

	stats := &ScanStats{}
	rootSize := int64(0)
	rootNode := m.scanner.RootNode
	if m.scanner != nil {
		stats = &m.scanner.Stats
		if rootNode != nil {
			rootSize = rootNode.Size
		}
	}

	sb.WriteString(RenderHeader(m.path, stats, w, m.sortLabel()))
	sb.WriteString("\n")

	treeFocused := m.focus.ActivePanel == TreePanel
	extFocused := m.focus.ActivePanel == ExtPanel

	treeView := RenderTree(m.flatTree, rootSize, m.focus.TreeCursor, treeFocused,
		m.viewDims.TreeW, m.viewDims.TreeH)
	extView := RenderExtensions(m.exts, m.focus.ExtCursor, extFocused,
		rootSize, m.viewDims.ExtW, m.viewDims.ExtH)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, treeView, extView)
	sb.WriteString(topRow)
	sb.WriteString("\n")

	treemapFocused := m.focus.ActivePanel == TreemapPanel
	treemapView := RenderTreemap(m.treemap, treemapFocused,
		m.viewDims.TreemapW, m.viewDims.TreemapH)
	sb.WriteString(treemapView)
	sb.WriteString("\n")

	sb.WriteString(RenderFooter(w))

	return sb.String()
}

func (m Model) renderInputScreen() string {
	var sb strings.Builder
	w, h := m.viewDims.Width, m.viewDims.Height
	stats := &ScanStats{}
	if m.scanner != nil {
		stats = &m.scanner.Stats
	}
	sb.WriteString(RenderHeader(m.path, stats, w, m.sortLabel()))
	sb.WriteString("\n\n\n")
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true).Render("  Scan path:")
	sb.WriteString(prompt + "\n\n  " + m.input.View())
	sb.WriteString("\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).Render("Enter to scan, Esc to cancel."))
	lines := strings.Count(sb.String(), "\n")
	for lines < h-2 {
		sb.WriteString("\n")
		lines++
	}
	sb.WriteString(RenderFooter(w))
	return sb.String()
}

func (m Model) renderModalScreen() string {
	var sb strings.Builder
	w, h := m.viewDims.Width, m.viewDims.Height
	stats := &ScanStats{}
	if m.scanner != nil {
		stats = &m.scanner.Stats
	}
	sb.WriteString(RenderHeader(m.path, stats, w, m.sortLabel()))
	sb.WriteString("\n")

	var title, body, hint string
	switch m.modal {
	case modalConfirmDelete:
		node := m.modalNode
		title = "Confirm delete"
		kind := "file"
		path := "?"
		size := "?"
		if node != nil {
			if node.IsDir {
				kind = "directory (recursive)"
			}
			if node.IsSymlink {
				kind = "symlink"
			}
			path = node.Path
			size = FormatSize(node.Size)
		}
		body = fmt.Sprintf("Delete this %s?\n\n  %s\n  Size: %s", kind, path, size)
		hint = "y: delete   n / esc: cancel"

	case modalMessage:
		title = "Message"
		body = m.modalMsg
		hint = "Press any key to dismiss"
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e")).Bold(true)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(1, 2).
		Width(minInt(w-6, 72))

	modalContent := titleStyle.Render(title) + "\n\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5")).Render(body) + "\n\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89")).Render(hint)
	rendered := boxStyle.Render(modalContent)

	availH := h - 2
	if availH < 1 {
		availH = 1
	}
	centered := lipgloss.Place(w, availH, lipgloss.Center, lipgloss.Center, rendered,
		lipgloss.WithWhitespaceChars(" "))
	sb.WriteString(centered)
	sb.WriteString("\n")
	sb.WriteString(RenderFooter(w))
	return sb.String()
}

// minInt is a tiny helper (kept explicit for clarity alongside ints).
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
