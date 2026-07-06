package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// scanUpdateMsg is sent periodically during scanning.
type scanUpdateMsg struct{}

// Model is the BubbleTea model for UnixDirStat.
type Model struct {
	scanner   *Scanner
	path      string
	exts      []*ExtGroup
	treemap   []TreemapItem
	focus     FocusState
	input     textinput.Model
	inputMode bool
	ready     bool
	viewDims  ViewDims
}

func NewModel(path string) Model {
	ti := textinput.New()
	ti.Placeholder = "/path/to/scan"
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 50

	return Model{
		path:    path,
		scanner: NewScanner(path),
		input:   ti,
		focus: FocusState{
			ActivePanel: TreePanel,
		},
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
		if m.inputMode {
			return m.handleInputMode(msg)
		}
		return m.handleKeyMsg(msg)

	case scanUpdateMsg:
		if m.scanner != nil {
			m.rebuildViews()
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
		m.scanner = NewScanner(m.path)
		m.exts = nil
		m.treemap = nil
		ch := m.scanner.Run()
		return m, tea.Batch(
			func() tea.Msg { <-ch; return scanUpdateMsg{} },
			m.pollUpdates(),
		)

	case "/":
		m.inputMode = true
		m.input.SetValue(m.path)
		return m, textinput.Blink

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
			m.scanner = NewScanner(path)
			m.exts = nil
			m.treemap = nil
			m.inputMode = false
			ch := m.scanner.Run()
			return m, tea.Batch(
				func() tea.Msg { <-ch; return scanUpdateMsg{} },
				m.pollUpdates(),
			)
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

func (m *Model) moveCursor(delta int) {
	switch m.focus.ActivePanel {
	case TreePanel:
		if m.scanner != nil && m.scanner.RootNode != nil {
			nodes := FlattenTree(m.scanner.RootNode, 20)
			m.focus.TreeCursor += delta
			if m.focus.TreeCursor < 0 {
				m.focus.TreeCursor = 0
			}
			if m.focus.TreeCursor >= len(nodes) {
				m.focus.TreeCursor = len(nodes) - 1
			}
			// Rebuild treemap for the node under cursor
			m.rebuildTreemapForSelection()
		}
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
	if m.focus.ActivePanel == TreePanel && m.scanner != nil && m.scanner.RootNode != nil {
		nodes := FlattenTree(m.scanner.RootNode, 20)
		if m.focus.TreeCursor >= 0 && m.focus.TreeCursor < len(nodes) {
			node := nodes[m.focus.TreeCursor].Node
			if node.IsDir {
				node.Expanded = !node.Expanded
			}
		}
	}
}

// rebuildTreemapForSelection rebuilds the treemap to show the contents
// of the currently selected directory in the tree.
func (m *Model) rebuildTreemapForSelection() {
	if m.scanner == nil || m.scanner.RootNode == nil {
		return
	}
	nodes := FlattenTree(m.scanner.RootNode, 20)
	if m.focus.TreeCursor < 0 || m.focus.TreeCursor >= len(nodes) {
		return
	}
	selected := nodes[m.focus.TreeCursor].Node

	// If the selected node is a directory, show its children
	target := selected
	if !selected.IsDir {
		// If it's a file, show its parent's children (highlight the file)
		if selected.Parent != nil {
			target = selected.Parent
		}
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
	if w < 40 || h < 15 {
		return
	}

	// Layout: header(1) + topPanels + treemap + footer(1) = h
	// topPanels = 40% of usable, treemap = 60%
	borderOverhead := 2 // top+bottom border lines per panel

	usableH := h - 2 // header + footer
	topH := usableH * 2 / 5
	bottomH := usableH - topH

	// Width: tree 60%, extensions 40%
	treeW := w * 3 / 5
	extW := w - treeW

	m.viewDims.TreeW = treeW
	m.viewDims.TreeH = topH - borderOverhead
	m.viewDims.ExtW = extW
	m.viewDims.ExtH = topH - borderOverhead
	m.viewDims.TreemapW = w
	m.viewDims.TreemapH = bottomH - borderOverhead
}

func (m *Model) rebuildViews() {
	if m.scanner == nil || m.scanner.RootNode == nil {
		return
	}

	if !m.scanner.RootNode.Expanded {
		m.scanner.RootNode.Expanded = true
	}

	m.exts = GroupByExtension(m.scanner.RootNode)
	m.rebuildTreemapForSelection()
}

func (m Model) View() string {
	if !m.ready {
		return "Loading…"
	}

	w, h := m.viewDims.Width, m.viewDims.Height

	if m.inputMode {
		var sb strings.Builder
		stats := &ScanStats{}
		if m.scanner != nil {
			stats = &m.scanner.Stats
		}
		sb.WriteString(RenderHeader(m.path, stats, w))
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

	var sb strings.Builder

	stats := &ScanStats{}
	rootSize := int64(0)
	rootNode := m.scanner.RootNode
	if m.scanner != nil {
		stats = &m.scanner.Stats
		if rootNode != nil {
			rootSize = rootNode.Size
		}
	}

	sb.WriteString(RenderHeader(m.path, stats, w))
	sb.WriteString("\n")

	treeFocused := m.focus.ActivePanel == TreePanel
	extFocused := m.focus.ActivePanel == ExtPanel

	treeView := RenderTree(rootNode, m.focus.TreeCursor, treeFocused,
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
