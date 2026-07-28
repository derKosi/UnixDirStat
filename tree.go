package main

import (
	"sort"
	"strings"
)

// maxTreeDepth bounds how deep the flattened tree view descends.
const maxTreeDepth = 20

// TreeNode represents a visible row in the tree view.
type TreeNode struct {
	Node   *FileNode
	Depth  int
	IsLast bool
	Prefix string // tree drawing prefix (│ ├ └ etc.)
}

// FlattenTree returns visible nodes for the tree view, respecting Expanded
// state. When showHidden is false, dotfiles are hidden.
// sortMode controls child ordering: size, name, or count.
func FlattenTree(root *FileNode, maxDepth int, showHidden bool, mode sortMode) []*TreeNode {
	var result []*TreeNode
	flattenRecursive(root, "", true, 0, maxDepth, showHidden, mode, &result)
	return result
}

func flattenRecursive(node *FileNode, prefix string, isLast bool, depth, maxDepth int, showHidden bool, mode sortMode, result *[]*TreeNode) {
	if node == nil || depth > maxDepth {
		return
	}

	tn := &TreeNode{
		Node:   node,
		Depth:  depth,
		IsLast: isLast,
		Prefix: prefix,
	}
	*result = append(*result, tn)

	if node.IsDir && node.Expanded && depth < maxDepth {
		children := make([]*FileNode, len(node.Children))
		copy(children, node.Children)
		sort.Slice(children, func(i, j int) bool {
			if children[i].IsDir != children[j].IsDir {
				return children[i].IsDir
			}
			switch mode {
			case sortByName:
				return children[i].Name < children[j].Name
			case sortByCount:
				return children[i].FileCount+children[i].DirCount > children[j].FileCount+children[j].DirCount
			default:
				return children[i].Size > children[j].Size
			}
		})

		for i, child := range children {
			if !showHidden && IsHidden(child.Name) {
				continue
			}
			childPrefix := prefix
			if depth > 0 {
				if isLast {
					childPrefix += "   "
				} else {
					childPrefix += "│  "
				}
			}
			childIsLast := i == len(children)-1
			flattenRecursive(child, childPrefix, childIsLast, depth+1, maxDepth, showHidden, mode, result)
		}
	}
}

// TreeConnector returns the tree-drawing character for a node.
func (tn *TreeNode) TreeConnector() string {
	if tn.Depth == 0 {
		return ""
	}
	if tn.IsLast {
		return "└──"
	}
	return "├──"
}

// ExpandIndicator returns + or - for directories, space for files.
func (tn *TreeNode) ExpandIndicator() string {
	if !tn.Node.IsDir {
		return " "
	}
	if tn.Node.Expanded {
		return "▾"
	}
	return "▸"
}

// ShortenPath shortens a path for display.
func ShortenPath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Keep the start and end
	home := "/home/"
	if strings.HasPrefix(path, home) {
		parts := strings.Split(path, "/")
		if len(parts) >= 4 {
			// Show ~/rest/of/path
			short := "~/" + strings.Join(parts[3:], "/")
			if len(short) <= maxLen {
				return short
			}
		}
	}
	// Truncate middle: "…" is 3 bytes (E2 80 A6), so result = half + 3 + half.
	// half + 3 + half <= maxLen  →  half <= (maxLen - 3) / 2
	half := (maxLen - 3) / 2
	if half < 1 {
		half = 1
	}
	return path[:half] + "…" + path[len(path)-half:]
}
