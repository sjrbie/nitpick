package app

import (
	"context"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sjrbie/nitpick/internal/config"
	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/gitlocal"
)

// fakeRepo is an in-memory forge.Repository that records created comments.
type fakeRepo struct {
	pr      forge.PullRequest
	created []forge.NewComment
}

func (f *fakeRepo) ListPullRequests(_ context.Context, _ forge.ListOpts) iter.Seq2[forge.PullRequest, error] {
	return func(yield func(forge.PullRequest, error) bool) { yield(f.pr, nil) }
}
func (f *fakeRepo) GetPullRequest(context.Context, int) (forge.PullRequest, error) {
	return f.pr, nil
}
func (f *fakeRepo) ListReviewComments(context.Context, int) ([]forge.ReviewComment, error) {
	return nil, nil
}
func (f *fakeRepo) CreateReviewComment(_ context.Context, _ int, c forge.NewComment) (forge.ReviewComment, error) {
	f.created = append(f.created, c)
	return forge.ReviewComment{ID: int64(len(f.created)), Path: c.Path, Line: c.Line, Body: c.Body}, nil
}
func (f *fakeRepo) ReplyToReviewComment(context.Context, int, int64, string) (forge.ReviewComment, error) {
	return forge.ReviewComment{}, nil
}

type fakeForge struct{ r *fakeRepo }

func (f fakeForge) Repo(string, string) forge.Repository { return f.r }

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestScrapeEndToEnd drives the full round-trip: markers in the working tree ->
// posted to the forge -> stripped from the file -> recorded in the stash.
func TestScrapeEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "feature")

	committed := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(committed), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "code.go")
	gitRun(t, dir, "commit", "-q", "-m", "initial")

	// Add an own-line marker above line2 in the working tree only.
	withMarker := "line1\n// nit: rework line2\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte(withMarker), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir) // Scrape resolves files relative to the working directory.

	head := gitHead(t, dir)
	fr := &fakeRepo{pr: forge.PullRequest{Number: 7, HeadRef: "feature", HeadSHA: head, State: forge.StateOpen}}
	a := New(fakeForge{fr}, config.Remote{Owner: "o", Name: "r"}, gitlocal.New("."))

	res, err := a.Scrape(context.Background(), false)
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failures)
	}
	if len(res.Posted) != 1 {
		t.Fatalf("posted %d, want 1", len(res.Posted))
	}

	// The comment posted at the committed line 2 with the marker body.
	if len(fr.created) != 1 {
		t.Fatalf("forge got %d comments, want 1", len(fr.created))
	}
	c := fr.created[0]
	if c.Path != "code.go" || c.Line != 2 || c.Body != "rework line2" {
		t.Errorf("posted comment = %+v, want code.go:2 'rework line2'", c)
	}
	if c.CommitID != head {
		t.Errorf("comment anchored to %q, want PR head %q", c.CommitID, head)
	}
	if c.Side != forge.SideRight {
		t.Errorf("side = %q, want RIGHT", c.Side)
	}

	// The marker was stripped, restoring the committed file.
	after, err := os.ReadFile(filepath.Join(dir, "code.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != committed {
		t.Errorf("file after strip = %q, want %q", after, committed)
	}

	// The stash recorded the posted comment.
	if _, err := os.Stat(filepath.Join(dir, ".nitpick", "stash.json")); err != nil {
		t.Errorf("stash not written: %v", err)
	}
}

// TestScrapeRejectsUnpushedHead reproduces the case where local HEAD is ahead
// of the PR: posting must fail early rather than forwarding a 422.
func TestScrapeRejectsUnpushedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "code.go")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("a\n// nit: x\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// PR head is some other commit that isn't local HEAD (branch not pushed).
	fr := &fakeRepo{pr: forge.PullRequest{
		Number: 1, HeadRef: "feature", State: forge.StateOpen,
		HeadSHA: "0000000000000000000000000000000000000000",
	}}
	a := New(fakeForge{fr}, config.Remote{Owner: "o", Name: "r"}, gitlocal.New("."))

	_, err := a.Scrape(context.Background(), false)
	if err == nil {
		t.Fatal("expected an error when local HEAD is not the PR head")
	}
	if len(fr.created) != 0 {
		t.Errorf("nothing should have been posted, got %d", len(fr.created))
	}
	// The file must be left untouched (no strip on a failed run).
	after, _ := os.ReadFile(filepath.Join(dir, "code.go"))
	if string(after) != "a\n// nit: x\nb\n" {
		t.Errorf("file was modified on a failed run: %q", after)
	}
}

// TestScrapeDryRun posts nothing and leaves the file untouched.
func TestScrapeDryRun(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "feature")
	withMarker := "code()\n// nit: think about this\nmore()\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(withMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "x.go")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	// Re-touch so it shows as changed vs HEAD (append a line locally).
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(withMarker+"tail()\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)
	fr := &fakeRepo{pr: forge.PullRequest{Number: 3, HeadRef: "feature", State: forge.StateOpen}}
	a := New(fakeForge{fr}, config.Remote{Owner: "o", Name: "r"}, gitlocal.New("."))

	res, err := a.Scrape(context.Background(), true)
	if err != nil {
		t.Fatalf("Scrape dry-run: %v", err)
	}
	if !res.DryRun || len(res.Posted) != 1 {
		t.Fatalf("dry-run result = %+v", res)
	}
	if len(fr.created) != 0 {
		t.Errorf("dry-run posted %d comments, want 0", len(fr.created))
	}
}
