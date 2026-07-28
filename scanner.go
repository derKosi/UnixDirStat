package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// FileNode represents a file or directory in the scan tree.
type FileNode struct {
	Name      string
	Path      string
	Size      int64
	IsDir     bool
	IsSymlink bool
	Children  []*FileNode
	Parent    *FileNode
	Expanded  bool

	// FileCount is the number of files (recursively) under this directory.
	// For files, this is 1. Populated during computeDirSizes.
	FileCount int64
	// DirCount is the number of subdirectories (recursively).
	// For files, this is 0. Populated during computeDirSizes.
	DirCount int64
}

// ScanStats tracks scan progress.
type ScanStats struct {
	FilesScanned    atomic.Int64
	DirsScanned     atomic.Int64
	SymlinksScanned atomic.Int64
	TotalSize       atomic.Int64
	Errors          atomic.Int64
	Done            atomic.Bool
	CurrentPath     atomic.Value // string
	LastError       atomic.Value // string
}

// ScanConfig configures scanner behaviour.
type ScanConfig struct {
	FollowSymlinks bool
	MaxWorkers     int
}

// DefaultScanConfig is applied when no config is supplied.
var DefaultScanConfig = ScanConfig{
	FollowSymlinks: false,
	MaxWorkers:     64,
}

func resolveConfig(cfgs ...ScanConfig) ScanConfig {
	cfg := DefaultScanConfig
	if len(cfgs) > 0 {
		c := cfgs[0]
		cfg.FollowSymlinks = c.FollowSymlinks
		if c.MaxWorkers > 0 {
			cfg.MaxWorkers = c.MaxWorkers
		}
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 64
	}
	return cfg
}

// Scanner walks a directory tree using a semaphore-bounded goroutine pool.
// Each directory gets its own goroutine, bounded by a semaphore channel.
// This avoids the deadlock that a shared job-channel causes when all workers
// simultaneously need to enqueue children.
type Scanner struct {
	Root     string
	RootNode *FileNode
	Stats    ScanStats
	cfg      ScanConfig

	visited map[string]struct{}
	visMu   sync.Mutex
}

func NewScanner(root string, cfgs ...ScanConfig) *Scanner {
	cfg := resolveConfig(cfgs...)
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &Scanner{
		Root:     abs,
		RootNode: &FileNode{Name: filepath.Base(abs), Path: abs, IsDir: true},
		Stats:    ScanStats{},
		cfg:      cfg,
		visited:  make(map[string]struct{}),
	}
}

// Run starts the scan and signals completion on the returned channel.
func (s *Scanner) Run() <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		s.scan()
		s.computeDirSizes(s.RootNode)
		s.Stats.Done.Store(true)
		select {
		case ch <- struct{}{}:
		default:
		}
	}()
	return ch
}

// scan uses a semaphore (buffered channel) to bound concurrent goroutines.
// Each directory spawns its own goroutine; the semaphore limits how many
// run concurrently. A WaitGroup tracks all outstanding directory goroutines.
// Deadlock-free because a goroutine releases its semaphore slot AFTER fully
// processing its directory (including enqueueing children).
func (s *Scanner) scan() {
	sem := make(chan struct{}, s.cfg.MaxWorkers)
	var wg sync.WaitGroup

	s.markVisited(s.Root)

	wg.Add(1)
	sem <- struct{}{}
	go s.scanDir(s.RootNode, sem, &wg)

	wg.Wait()
}

// scanDir reads one directory and recursively scans subdirectories.
// The semaphore slot is released BEFORE spawning children to prevent
// deadlock: if the parent held its slot while acquiring child slots,
// all workers could block on sem <- struct{}{} with none releasing.
func (s *Scanner) scanDir(node *FileNode, sem chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	s.Stats.CurrentPath.Store(node.Path)

	entries, err := os.ReadDir(node.Path)
	if err != nil {
		s.recordError(node.Path, err)
		return
	}

	var subdirs []*FileNode

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(node.Path, name)
		isLink := entry.Type()&os.ModeSymlink != 0

		if isLink && s.cfg.FollowSymlinks {
			fi, ferr := os.Stat(fullPath)
			if ferr != nil {
				s.recordError(fullPath, ferr)
				s.addSymlinkNode(node, name, fullPath, entry)
				continue
			}
			if fi.IsDir() {
				if s.markVisited(fullPath) {
					continue
				}
				child := &FileNode{
					Name: name, Path: fullPath,
					IsDir: true, IsSymlink: true, Parent: node,
				}
				node.Children = append(node.Children, child)
				s.Stats.DirsScanned.Add(1)
				s.Stats.SymlinksScanned.Add(1)
				subdirs = append(subdirs, child)
				continue
			}
			child := &FileNode{
				Name: name, Path: fullPath,
				Size: fi.Size(), IsSymlink: true, Parent: node,
			}
			node.Children = append(node.Children, child)
			s.Stats.FilesScanned.Add(1)
			s.Stats.SymlinksScanned.Add(1)
			s.Stats.TotalSize.Add(fi.Size())
			continue
		}

		if isLink {
			s.addSymlinkNode(node, name, fullPath, entry)
			continue
		}

		if entry.IsDir() {
			child := &FileNode{
				Name: name, Path: fullPath,
				IsDir: true, Parent: node,
			}
			node.Children = append(node.Children, child)
			s.Stats.DirsScanned.Add(1)
			subdirs = append(subdirs, child)
			continue
		}

		size := int64(0)
		if info, ierr := entry.Info(); ierr == nil {
			size = info.Size()
		}
		child := &FileNode{Name: name, Path: fullPath, Size: size, Parent: node}
		node.Children = append(node.Children, child)
		s.Stats.FilesScanned.Add(1)
		s.Stats.TotalSize.Add(size)
	}

	// Release semaphore BEFORE spawning children. This is critical:
	// the parent no longer needs its slot (its directory is fully read),
	// and releasing first means children can acquire slots even if the
	// pool is full.
	<-sem

	for _, child := range subdirs {
		wg.Add(1)
		sem <- struct{}{} // acquire slot for child
		go s.scanDir(child, sem, wg)
	}
}

func (s *Scanner) addSymlinkNode(node *FileNode, name, fullPath string, entry os.DirEntry) {
	size := int64(0)
	if info, err := entry.Info(); err == nil {
		size = info.Size()
	}
	child := &FileNode{
		Name: name, Path: fullPath,
		Size: size, IsSymlink: true, Parent: node,
	}
	node.Children = append(node.Children, child)
	s.Stats.SymlinksScanned.Add(1)
	s.Stats.TotalSize.Add(size)
}

// markVisited records a path as visited and reports whether it was seen before.
// Used for symlink cycle detection in follow mode. The path is resolved to
// its canonical form via EvalSymlinks so that two symlinks pointing at the
// same target are detected as a cycle.
func (s *Scanner) markVisited(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	s.visMu.Lock()
	defer s.visMu.Unlock()
	if _, ok := s.visited[resolved]; ok {
		return true
	}
	s.visited[resolved] = struct{}{}
	return false
}

func (s *Scanner) recordError(path string, err error) {
	s.Stats.Errors.Add(1)
	s.Stats.LastError.Store(fmt.Sprintf("%s: %v", path, err))
}

// computeDirSizes calculates directory sizes and file/dir counts (post-order).
// Each directory node counts itself in DirCount, so a dir with 3 subdirs
// has DirCount=3 (subdirs) — the node's own existence is implicit (it's a dir).
func (s *Scanner) computeDirSizes(node *FileNode) (int64, int64, int64) {
	if !node.IsDir {
		return node.Size, 1, 0
	}
	var totalSize, fileCount, dirCount int64
	for _, child := range node.Children {
		cs, fc, dc := s.computeDirSizes(child)
		totalSize += cs
		fileCount += fc
		// Count this child as a directory if it is one.
		if child.IsDir {
			dirCount += dc + 1
		} else {
			dirCount += dc
		}
	}
	node.Size = totalSize
	node.FileCount = fileCount
	node.DirCount = dirCount
	return totalSize, fileCount, dirCount
}
