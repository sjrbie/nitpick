package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripMarkersRestoresCommittedFile(t *testing.T) {
	dir := t.TempDir()
	// One own-line marker (whole line removed) and one trailing marker (code kept).
	content := "package p\n\n// nit: add nil check\nfunc F() {}\nx := 1 // nit: rename x\n"
	writeF(t, filepath.Join(dir, "f.go"), content)

	markers := ParseFile("f.go", content)
	if len(markers) != 2 {
		t.Fatalf("expected 2 markers, got %d", len(markers))
	}

	if err := StripMarkers(dir, markers); err != nil {
		t.Fatalf("StripMarkers: %v", err)
	}

	got := readF(t, filepath.Join(dir, "f.go"))
	want := "package p\n\nfunc F() {}\nx := 1\n"
	if got != want {
		t.Fatalf("stripped file =\n%q\nwant\n%q", got, want)
	}

	// The committed line number each marker reported must point at the right
	// content in the stripped file.
	strippedLines := strings.Split(got, "\n")
	for _, m := range markers {
		line := strippedLines[m.Line-1]
		if strings.Contains(line, "nit:") {
			t.Errorf("marker line %d still contains a marker: %q", m.Line, line)
		}
	}
}

func TestCollectConcurrent(t *testing.T) {
	dir := t.TempDir()
	writeF(t, filepath.Join(dir, "a.go"), "code()\n// nit: note A\nmore()\n")
	writeF(t, filepath.Join(dir, "b.go"), "x := 1 // nit: note B\n")
	writeF(t, filepath.Join(dir, "c.go"), "no markers here\n")

	src := fakeFiles{"a.go", "b.go", "c.go", "missing.go"}
	markers, err := Collect(context.Background(), src, dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(markers) != 2 {
		t.Fatalf("got %d markers, want 2: %+v", len(markers), markers)
	}
	// Sorted by path: a.go then b.go.
	if markers[0].Path != "a.go" || markers[0].Body != "note A" {
		t.Errorf("markers[0] = %+v", markers[0])
	}
	if markers[1].Path != "b.go" || markers[1].Body != "note B" {
		t.Errorf("markers[1] = %+v", markers[1])
	}
}

type fakeFiles []string

func (f fakeFiles) ChangedFiles(context.Context) ([]string, error) { return []string(f), nil }

func writeF(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readF(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
