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
	// FollowSymlinks recurses into symlinked directories. Cycle protection
	// is enabled automatically. Default false (symlinks are recorded but
	// not followed, which is safe and loop-free by construction).
	FollowSymlinks bool
	// MaxWorkers bounds concurrent directory reads. <=0 → 64.
	MaxWorkers int
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

// jobsBuf is generously sized: the job channel only ever carries
// directories (files are handled inline), so fan-in per directory is the
// number of immediate sub-directories — modest in practice.
const jobsBuf = 4096

// Scanner walks a directory tree using a bounded worker pool, preventing
// file-descriptor exhaustion and unbounded goroutine growth on huge trees.
type Scanner struct {
	Root     string
	RootNode *FileNode
	Stats    ScanStats
	cfg      ScanConfig

	// cycle protection (follow-symlinks mode only): real paths already visited.
	visited map[string]struct{}
	visMu   sync.Mutex
}

// NewScanner creates a scanner. Variadic config keeps existing callers
// (e.g. tests) working with sane defaults.
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

// scan drives a fixed-size worker pool fed by a job channel. The closer
// pattern (pending.Wait() → close(jobs)) guarantees all directories are
// processed before the channel closes. pending.Add for children happens
// before the corresponding Done for the parent, so the counter never
// reaches zero prematurely.
func (s *Scanner) scan() {
	workers := s.cfg.MaxWorkers
	jobs := make(chan *FileNode, jobsBuf)
	var pending sync.WaitGroup // outstanding directory jobs

	for i := 0; i < workers; i++ {
		go s.worker(jobs, &pending)
	}

	// Seed root into the visited set so a symlink pointing back at the
	// scan root is detected as a cycle when follow mode is on.
	s.markVisited(s.Root)

	pending.Add(1)
	jobs <- s.RootNode

	pending.Wait()
	close(jobs)
}

func (s *Scanner) worker(jobs chan *FileNode, pending *sync.WaitGroup) {
	for node := range jobs {
		s.processDir(node, jobs, pending)
		pending.Done()
	}
}

// processDir reads one directory's entries and appends child nodes. Each
// child directory is counted and enqueued; files are counted inline.
// A node is ever processed by exactly one worker, so node.Children needs
// no external synchronisation.
func (s *Scanner) processDir(node *FileNode, jobs chan<- *FileNode, pending *sync.WaitGroup) {
	s.Stats.CurrentPath.Store(node.Path)

	entries, err := os.ReadDir(node.Path)
	if err != nil {
		s.recordError(node.Path, err)
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(node.Path, name)
		isLink := entry.Type()&os.ModeSymlink != 0

		// Followed symlink: resolve to its target.
		if isLink && s.cfg.FollowSymlinks {
			fi, ferr := os.Stat(fullPath)
			if ferr != nil {
				// Broken or unreadable symlink: record as a link node + error.
				s.recordError(fullPath, ferr)
				s.addSymlinkNode(node, name, fullPath, entry)
				continue
			}
			if fi.IsDir() {
				if s.markVisited(fullPath) {
					continue // cycle — skip
				}
				child := &FileNode{
					Name: name, Path: fullPath,
					IsDir: true, IsSymlink: true, Parent: node,
				}
				node.Children = append(node.Children, child)
				s.Stats.DirsScanned.Add(1)
				s.Stats.SymlinksScanned.Add(1)
				pending.Add(1)
				jobs <- child
				continue
			}
			// symlink → file
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

		// Symlink, not followed: record the link itself (small) so it is visible.
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
			pending.Add(1)
			jobs <- child
			continue
		}

		// Regular (or other non-dir, non-link) file.
		size := int64(0)
		if info, ierr := entry.Info(); ierr == nil {
			size = info.Size()
		}
		child := &FileNode{Name: name, Path: fullPath, Size: size, Parent: node}
		node.Children = append(node.Children, child)
		s.Stats.FilesScanned.Add(1)
		s.Stats.TotalSize.Add(size)
	}
}

// addSymlinkNode appends a not-followed symlink node, counting it and its
// (link) size. The link's own size is tiny; it is shown mainly so the
// entry is visible and deletable from the UI.
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

// markVisited records a resolved path and reports whether it was already
// seen (true == cycle). Used only in follow-symlink mode.
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

// computeDirSizes calculates directory sizes from children (post-order).
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
