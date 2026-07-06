package review

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStashRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Missing stash loads empty.
	s, err := LoadStash(dir)
	if err != nil {
		t.Fatalf("LoadStash (missing): %v", err)
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected empty stash, got %d entries", len(s.Entries))
	}

	s.Add(StashEntry{
		PR: 7, Path: "f.go", Line: 12, Body: "fix this",
		CommitID: "abc123", CommentID: 999, PostedAt: time.Unix(1000, 0).UTC(),
	})
	if err := s.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, StashDir, "stash.json")); err != nil {
		t.Fatalf("stash file not written: %v", err)
	}

	got, err := LoadStash(dir)
	if err != nil {
		t.Fatalf("LoadStash: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(got.Entries))
	}
	e := got.Entries[0]
	if e.PR != 7 || e.Path != "f.go" || e.Line != 12 || e.CommentID != 999 {
		t.Errorf("round-tripped entry mismatch: %+v", e)
	}
}
