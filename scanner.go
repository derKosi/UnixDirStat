package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// FileNode represents a file or directory in the scan tree.
type FileNode struct {
	Name     string
	Path     string
	Size     int64
	IsDir    bool
	Children []*FileNode
	Parent   *FileNode
	Expanded bool
}

// ScanStats tracks scan progress.
type ScanStats struct {
	FilesScanned atomic.Int64
	DirsScanned  atomic.Int64
	TotalSize    atomic.Int64
	Done         atomic.Bool
	CurrentPath  atomic.Value // string
}

// Scanner walks a directory tree.
type Scanner struct {
	Root     string
	RootNode *FileNode
	Stats    ScanStats
}

func NewScanner(root string) *Scanner {
	abs, _ := filepath.Abs(root)
	return &Scanner{
		Root: abs,
		RootNode: &FileNode{
			Name:  filepath.Base(abs),
			Path:  abs,
			IsDir: true,
		},
	}
}

// Run starts the scan. Sends a signal on the channel when done.
func (s *Scanner) Run() <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		s.walk(s.RootNode)
		s.computeDirSizes(s.RootNode)
		s.Stats.Done.Store(true)
		select {
		case ch <- struct{}{}:
		default:
		}
	}()
	return ch
}

func (s *Scanner) walk(node *FileNode) {
	entries, err := os.ReadDir(node.Path)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, entry := range entries {
		fullPath := filepath.Join(node.Path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if entry.IsDir() {
			s.Stats.DirsScanned.Add(1)
			s.Stats.CurrentPath.Store(fullPath)

			child := &FileNode{
				Name:     entry.Name(),
				Path:     fullPath,
				IsDir:    true,
				Expanded: false,
				Parent:   node,
			}

			mu.Lock()
			node.Children = append(node.Children, child)
			mu.Unlock()

			wg.Add(1)
			go func(c *FileNode) {
				defer wg.Done()
				s.walk(c)
			}(child)
		} else {
			size := info.Size()
			s.Stats.FilesScanned.Add(1)
			s.Stats.TotalSize.Add(size)

			child := &FileNode{
				Name:   entry.Name(),
				Path:   fullPath,
				Size:   size,
				IsDir:  false,
				Parent: node,
			}

			mu.Lock()
			node.Children = append(node.Children, child)
			mu.Unlock()
		}
	}

	wg.Wait()
}

// computeDirSizes calculates directory sizes from children.
func (s *Scanner) computeDirSizes(node *FileNode) int64 {
	if !node.IsDir {
		return node.Size
	}
	var total int64
	for _, child := range node.Children {
		total += s.computeDirSizes(child)
	}
	node.Size = total
	return total
}
