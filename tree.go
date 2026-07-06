package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// TreeNode represents a visible row in the tree view.
type TreeNode struct {
	Node     *FileNode
	Depth    int
	IsLast   bool
	Prefix   string // tree drawing prefix (│ ├ └ etc.)
}

// FlattenTree returns visible nodes for the tree view, respecting Expanded state.
func FlattenTree(root *FileNode, maxDepth int) []*TreeNode {
	var result []*TreeNode
	flattenRecursive(root, "", true, 0, maxDepth, &result)
	return result
}

func flattenRecursive(node *FileNode, prefix string, isLast bool, depth, maxDepth int, result *[]*TreeNode) {
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
		// Sort children: directories first, then by size descending
		children := make([]*FileNode, len(node.Children))
		copy(children, node.Children)
		sort.Slice(children, func(i, j int) bool {
			if children[i].IsDir != children[j].IsDir {
				return children[i].IsDir
			}
			return children[i].Size > children[j].Size
		})

		for i, child := range children {
			childPrefix := prefix
			if depth > 0 {
				if isLast {
					childPrefix += "   "
				} else {
					childPrefix += "│  "
				}
			}
			childIsLast := i == len(children)-1
			flattenRecursive(child, childPrefix, childIsLast, depth+1, maxDepth, result)
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

// RelPath returns the display name relative to the scan root.
func RelPath(node *FileNode, rootPath string) string {
	if node.Path == rootPath {
		return filepath.Base(rootPath)
	}
	rel, err := filepath.Rel(rootPath, node.Path)
	if err != nil {
		return node.Name
	}
	return rel
}

// FindNodeAtPath finds the node at the given tree index and toggles its expansion.
func FindNodeByIndex(root *FileNode, index int) *FileNode {
	visible := FlattenTree(root, 20)
	if index < 0 || index >= len(visible) {
		return nil
	}
	return visible[index].Node
}

// CountChildren returns the number of direct children.
func CountChildren(node *FileNode) int {
	if node == nil {
		return 0
	}
	return len(node.Children)
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
	// Truncate middle
	half := maxLen / 2
	return path[:half] + "…" + path[len(path)-half:]
}
