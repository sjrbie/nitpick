package gitlocal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
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

// TestGitlocalIntegration drives the real git subcommands end-to-end against a
// throwaway repository.
func TestGitlocalIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")

	path := filepath.Join(dir, "code.go")
	writeFile(t, path, "line1\nline2\nline3\nline4\n")
	runGit(t, dir, "add", "code.go")
	runGit(t, dir, "commit", "-q", "-m", "initial")

	r := New(dir)
	ctx := context.Background()

	branch, err := r.CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	files, err := r.TrackedFiles(ctx)
	if err != nil {
		t.Fatalf("TrackedFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "code.go" {
		t.Errorf("TrackedFiles = %v, want [code.go]", files)
	}

	// Modify line 2 in the working tree; commit stays as the reference point.
	writeFile(t, path, "line1\nCHANGED\nline3\nline4\n")

	changed, err := r.ChangedLinesSince(ctx, "HEAD", []string{"code.go"})
	if err != nil {
		t.Fatalf("ChangedLinesSince: %v", err)
	}
	ranges := changed["code.go"]
	if len(ranges) != 1 || !ranges[0].Contains(2) {
		t.Errorf("changed ranges = %v, want one range covering line 2", ranges)
	}
	if ranges[0].Contains(4) {
		t.Errorf("line 4 should be unchanged, got %v", ranges)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
