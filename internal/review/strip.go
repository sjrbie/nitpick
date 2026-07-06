package review

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StripMarkers rewrites files to remove the given markers: own-line markers are
// deleted entirely, trailing markers are reduced to their preserved code. Only
// the passed markers (typically the ones successfully posted) are removed, so a
// marker whose post failed stays in place for a retry. dir is the repo root
// that marker paths are relative to.
func StripMarkers(dir string, markers []Marker) error {
	byFile := make(map[string][]Marker)
	for _, m := range markers {
		byFile[m.Path] = append(byFile[m.Path], m)
	}
	for path, ms := range byFile {
		if err := stripFile(filepath.Join(dir, path), ms); err != nil {
			return err
		}
	}
	return nil
}

func stripFile(fullPath string, markers []Marker) error {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	content := string(data)
	// Preserve a trailing newline: splitting "a\n" yields ["a",""]; we rejoin
	// with the same empty final element intact.
	lines := strings.Split(content, "\n")

	// remove[idx] deletes the line; replace[idx] overwrites it (trailing marker).
	remove := make(map[int]bool)
	replace := make(map[int]string)
	for _, m := range markers {
		switch m.kind {
		case ownLine:
			for i := m.startIdx; i < m.endIdx; i++ {
				remove[i] = true
			}
		case trailing:
			replace[m.startIdx] = m.keepCode
		}
	}

	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if remove[i] {
			continue
		}
		if r, ok := replace[i]; ok {
			out = append(out, r)
			continue
		}
		out = append(out, l)
	}

	// Write atomically via a temp file + rename to avoid a torn file.
	return writeFileAtomic(fullPath, []byte(strings.Join(out, "\n")))
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nitpick-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// sortMarkers orders markers by path then line, for stable output.
func sortMarkers(ms []Marker) {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Path != ms[j].Path {
			return ms[i].Path < ms[j].Path
		}
		return ms[i].Line < ms[j].Line
	})
}
