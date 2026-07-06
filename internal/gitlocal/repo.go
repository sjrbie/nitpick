// Package gitlocal inspects the local git working tree via os/exec, so Nitpick
// carries no VCS-library dependency. It answers two questions M1 needs: which
// files are tracked (for scraping later) and which lines have changed since a
// given commit (for tracking whether review feedback has been addressed).
package gitlocal

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Repo is a handle to a git working tree rooted at Dir.
type Repo struct {
	dir string
}

// New returns a Repo rooted at dir ("." for the current directory).
func New(dir string) *Repo {
	if dir == "" {
		dir = "."
	}
	return &Repo{dir: dir}
}

// LineRange is an inclusive span of line numbers within a file.
type LineRange struct {
	Start int
	End   int
}

// Contains reports whether line falls within the range.
func (r LineRange) Contains(line int) bool {
	return line >= r.Start && line <= r.End
}

// git runs a git subcommand in the repo directory and returns stdout.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", r.dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// CurrentBranch returns the checked-out branch name (or "HEAD" when detached).
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// TrackedFiles lists repo-relative paths under version control.
func (r *Repo) TrackedFiles(ctx context.Context) ([]string, error) {
	out, err := r.git(ctx, "ls-files")
	if err != nil {
		return nil, err
	}
	var files []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			files = append(files, line)
		}
	}
	return files, sc.Err()
}

// ChangedLinesSince returns, per path, the ranges of lines in ref's version of
// each file that have since been modified or deleted in the working tree. The
// ranges are expressed in ref's coordinate system (the diff's "old" side),
// which matches how a review comment anchored against that commit numbers its
// lines. When paths is empty the whole tree is diffed.
func (r *Repo) ChangedLinesSince(ctx context.Context, ref string, paths []string) (map[string][]LineRange, error) {
	args := []string{"diff", "--unified=0", "--no-color", ref}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseUnifiedOldRanges(out), nil
}

// parseUnifiedOldRanges parses `git diff --unified=0` output into per-path
// ranges on the old (pre-image) side. Only hunks that remove or modify existing
// lines contribute a range; pure insertions (old count 0) are ignored because
// they do not alter any pre-existing commented line.
func parseUnifiedOldRanges(diff string) map[string][]LineRange {
	result := make(map[string][]LineRange)
	var current string

	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			current = trimDiffPath(line[len("+++ "):])
		case strings.HasPrefix(line, "@@"):
			start, count, ok := parseOldHunkSpan(line)
			if !ok || current == "" || count == 0 {
				continue
			}
			result[current] = append(result[current], LineRange{Start: start, End: start + count - 1})
		}
	}
	return result
}

// trimDiffPath strips the "b/" (or "a/") prefix and any trailing tab metadata
// from a diff header path. "/dev/null" is returned as-is.
func trimDiffPath(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	if p == "/dev/null" {
		return p
	}
	if len(p) > 2 && (p[1] == '/') && (p[0] == 'a' || p[0] == 'b') {
		return p[2:]
	}
	return p
}

// parseOldHunkSpan extracts the old-side start line and count from a hunk
// header of the form "@@ -oldStart[,oldCount] +newStart[,newCount] @@".
func parseOldHunkSpan(header string) (start, count int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "-") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(fields[1], "-")
	startStr, countStr, hasCount := strings.Cut(spec, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if hasCount {
		count, err = strconv.Atoi(countStr)
		if err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}
