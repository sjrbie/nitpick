package review

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// FileSource lists the repo-relative files that may contain markers. It is
// backed by gitlocal (changed files vs HEAD) in production and faked in tests.
type FileSource interface {
	ChangedFiles(ctx context.Context) ([]string, error)
}

// Collect scans the source files concurrently and returns all markers found,
// sorted by path and line. dir is the repo root that paths are relative to.
// Files that cannot be read (e.g. deleted in the working tree) are skipped.
func Collect(ctx context.Context, src FileSource, dir string) ([]Marker, error) {
	files, err := src.ChangedFiles(ctx)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	workers := min(runtime.GOMAXPROCS(0), len(files))
	paths := make(chan string)

	var (
		mu  sync.Mutex
		all []Marker
		wg  sync.WaitGroup
	)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for p := range paths {
				markers := scrapeOne(dir, p)
				if len(markers) == 0 {
					continue
				}
				mu.Lock()
				all = append(all, markers...)
				mu.Unlock()
			}
		}()
	}

	// Feed paths, honoring cancellation.
	go func() {
		defer close(paths)
		for _, f := range files {
			select {
			case <-ctx.Done():
				return
			case paths <- f:
			}
		}
	}()
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sortMarkers(all)
	return all, nil
}

// scrapeOne reads a single file and parses its markers, returning nil on any
// read error (the file may have been deleted or be unreadable).
func scrapeOne(dir, path string) []Marker {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return nil
	}
	return ParseFile(path, string(data))
}
