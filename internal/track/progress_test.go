package track

import (
	"context"
	"errors"
	"testing"

	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/gitlocal"
)

// fakeSource returns canned changed ranges per ref, or an error for refs listed
// in fail.
type fakeSource struct {
	byRef map[string]map[string][]gitlocal.LineRange
	fail  map[string]bool
}

func (f fakeSource) ChangedLinesSince(_ context.Context, ref string, _ []string) (map[string][]gitlocal.LineRange, error) {
	if f.fail[ref] {
		return nil, errors.New("commit not found")
	}
	return f.byRef[ref], nil
}

func TestAssess(t *testing.T) {
	comments := []forge.ReviewComment{
		{ID: 1, Path: "a.go", Line: 10, CommitID: "c1"}, // line changed -> addressed
		{ID: 2, Path: "a.go", Line: 40, CommitID: "c1"}, // unchanged -> open
		{ID: 3, Path: "b.go", Line: 5, CommitID: "c2"},  // diff fails -> unknown
		{ID: 4, Path: "a.go", Line: 0, CommitID: "c1"},  // unlocated -> unknown
		{ID: 5, Path: "a.go", Line: 12, CommitID: ""},   // no commit -> unknown
	}
	src := fakeSource{
		byRef: map[string]map[string][]gitlocal.LineRange{
			"c1": {"a.go": {{Start: 8, End: 12}}},
		},
		fail: map[string]bool{"c2": true},
	}

	got := Assess(context.Background(), src, comments)
	if len(got) != len(comments) {
		t.Fatalf("got %d results, want %d", len(got), len(comments))
	}

	wantStatus := map[int64]Status{
		1: StatusAddressed,
		2: StatusOpen,
		3: StatusUnknown,
		4: StatusUnknown,
		5: StatusUnknown,
	}
	for _, cp := range got {
		if want := wantStatus[cp.Comment.ID]; cp.Status != want {
			t.Errorf("comment %d: got %s, want %s", cp.Comment.ID, cp.Status, want)
		}
	}

	sum := Summarize(got)
	if sum.Addressed != 1 || sum.Open != 1 || sum.Unknown != 3 {
		t.Errorf("summary = %+v, want {Addressed:1 Open:1 Unknown:3}", sum)
	}
}

func TestAssessPreservesOrder(t *testing.T) {
	comments := []forge.ReviewComment{
		{ID: 10, Path: "z.go", Line: 1, CommitID: "c1"},
		{ID: 20, Path: "z.go", Line: 2, CommitID: "c1"},
		{ID: 30, Path: "z.go", Line: 3, CommitID: "c1"},
	}
	src := fakeSource{byRef: map[string]map[string][]gitlocal.LineRange{"c1": {}}}
	got := Assess(context.Background(), src, comments)
	for i, cp := range got {
		if cp.Comment.ID != comments[i].ID {
			t.Errorf("order not preserved at %d: got %d", i, cp.Comment.ID)
		}
	}
}
