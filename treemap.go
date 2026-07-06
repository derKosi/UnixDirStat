package main

import "hash/fnv"

// Rect represents a rectangle in the treemap.
type Rect struct {
	X, Y, W, H int
}

// TreemapItem is a sized item to lay out.
type TreemapItem struct {
	Node  *FileNode
	Size  int64
	Color string
	Rect  Rect
}

// Squarify implements the squarified treemap algorithm.
// Returns a slice of items with their rectangles filled in.
func Squarify(items []TreemapItem, bounds Rect) []TreemapItem {
	if len(items) == 0 {
		return items
	}

	// Filter out zero-size items
	var sized []TreemapItem
	for _, item := range items {
		if item.Size > 0 {
			sized = append(sized, item)
		}
	}
	if len(sized) == 0 {
		return sized
	}

	// Total area is bounds.W * bounds.H
	totalArea := bounds.W * bounds.H
	if totalArea <= 0 {
		return sized
	}

	// Calculate total size for proportional area
	var totalSize int64
	for _, item := range sized {
		totalSize += item.Size
	}
	if totalSize == 0 {
		return sized
	}

	// Layout rows
	layoutRows(sized, bounds, totalSize, totalArea)

	return sized
}

func layoutRows(items []TreemapItem, bounds Rect, totalSize int64, totalArea int) {
	if len(items) == 0 || bounds.W <= 0 || bounds.H <= 0 || totalSize <= 0 {
		return
	}

	shortSide := bounds.W
	if bounds.H < shortSide {
		shortSide = bounds.H
	}

	// Find how many items fit in the first row
	var rowEnd int
	var rowSize int64
	bestAspect := worstAspect(items[:1], shortSide, totalSize, totalArea)

	for i := 1; i < len(items); i++ {
		candidate := items[:i+1]
		aspect := worstAspect(candidate, shortSide, totalSize, totalArea)
		if aspect <= bestAspect {
			bestAspect = aspect
			rowEnd = i
		} else {
			break
		}
	}
	rowEnd++ // exclusive

	row := items[:rowEnd]
	remaining := items[rowEnd:]

	// Calculate row size
	for _, item := range row {
		rowSize += item.Size
	}

	// Layout the row
	if bounds.W >= bounds.H {
		// Horizontal row
		rowWidth := int(float64(bounds.W) * float64(rowSize) / float64(totalSize))
		if rowWidth <= 0 {
			rowWidth = 1
		}
		layoutRow(row, bounds.X, bounds.Y, rowWidth, bounds.H)
		newBounds := Rect{bounds.X + rowWidth, bounds.Y, bounds.W - rowWidth, bounds.H}
		layoutRows(remaining, newBounds, totalSize-rowSize, totalArea)
	} else {
		// Vertical row
		rowHeight := int(float64(bounds.H) * float64(rowSize) / float64(totalSize))
		if rowHeight <= 0 {
			rowHeight = 1
		}
		layoutRow(row, bounds.X, bounds.Y, bounds.W, rowHeight)
		newBounds := Rect{bounds.X, bounds.Y + rowHeight, bounds.W, bounds.H - rowHeight}
		layoutRows(remaining, newBounds, totalSize-rowSize, totalArea)
	}
}

func layoutRow(row []TreemapItem, x, y, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}

	var totalSize int64
	for _, item := range row {
		totalSize += item.Size
	}
	if totalSize == 0 {
		return
	}

	offset := 0
	for i, item := range row {
		if w >= h {
			// Horizontal subdivision
			itemW := int(float64(w) * float64(item.Size) / float64(totalSize))
			if i == len(row)-1 {
				itemW = w - offset // ensure we fill exactly
			}
			if itemW < 0 {
				itemW = 0
			}
			item.Rect = Rect{x + offset, y, itemW, h}
			offset += itemW
		} else {
			// Vertical subdivision
			itemH := int(float64(h) * float64(item.Size) / float64(totalSize))
			if i == len(row)-1 {
				itemH = h - offset
			}
			if itemH < 0 {
				itemH = 0
			}
			item.Rect = Rect{x, y + offset, w, itemH}
			offset += itemH
		}
		row[i] = item
	}
}

// worstAspect computes the worst (highest) aspect ratio for a row.
func worstAspect(row []TreemapItem, shortSide int, totalSize int64, totalArea int) float64 {
	if len(row) == 0 || shortSide <= 0 || totalSize <= 0 || totalArea <= 0 {
		return 1e9
	}

	var rowSize int64
	for _, item := range row {
		rowSize += item.Size
	}

	rowArea := float64(totalArea) * float64(rowSize) / float64(totalSize)
	rowLength := float64(shortSide) * rowArea / float64(totalArea)

	if rowLength <= 0 {
		return 1e9
	}

	worst := 0.0
	for _, item := range row {
		itemArea := rowArea * float64(item.Size) / float64(rowSize)
		if itemArea <= 0 {
			continue
		}
		itemWidth := itemArea / rowLength
		aspect := max(rowLength/itemWidth, itemWidth/rowLength)
		if aspect > worst {
			worst = aspect
		}
	}
	return worst
}

// BuildTreemapItems flattens the tree into treemap items.
// If node is a directory with children, shows its children.
// Each item gets a vibrant color — directories included.
func BuildTreemapItems(node *FileNode) []TreemapItem {
	if node == nil {
		return nil
	}
	items := make([]TreemapItem, 0, len(node.Children))
	for _, child := range node.Children {
		var color string
		if !child.IsDir {
			ext := ""
			for i := len(child.Name) - 1; i >= 0; i-- {
				if child.Name[i] == '.' {
					ext = child.Name[i:]
					break
				}
			}
			color = ExtColor(ext)
		} else {
			// Directories get a vibrant color based on their name hash
			color = dirColor(child.Name)
		}
		items = append(items, TreemapItem{
			Node:  child,
			Size:  child.Size,
			Color: color,
		})
	}
	return items
}

// dirColor returns a vibrant color for a directory name.
var dirPalette = []string{
	"#565f89", "#7aa2f7", "#bb9af7", "#7dcfff",
	"#2ac3de", "#73daca", "#9ece6a", "#e0af68",
	"#ff9e64", "#f7768e", "#ff007c", "#d4a017",
}

func dirColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return dirPalette[h.Sum32()%uint32(len(dirPalette))]
}
