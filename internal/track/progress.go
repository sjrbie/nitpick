// Package track estimates how much review feedback has been addressed on the
// local branch. It is a heuristic: a comment is considered addressed when the
// line it was anchored to has been modified since the commit it was left
// against. Results are hints, not guarantees.
package track

import (
	"context"

	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/gitlocal"
)

// Status is the assessed state of a single review comment.
type Status string

const (
	// StatusAddressed means the commented line was changed locally since the
	// comment's commit.
	StatusAddressed Status = "addressed"
	// StatusOpen means the commented line is unchanged since the comment.
	StatusOpen Status = "open"
	// StatusUnknown means progress could not be determined (e.g. the comment's
	// commit is not present locally, or the comment is outdated/unlocated).
	StatusUnknown Status = "unknown"
)

// CommentProgress pairs a review comment with its assessed status.
type CommentProgress struct {
	Comment forge.ReviewComment
	Status  Status
}

// ChangeSource reports which lines changed in a file since a git ref. It is the
// single dependency Assess needs, which keeps it testable without a real repo.
type ChangeSource interface {
	ChangedLinesSince(ctx context.Context, ref string, paths []string) (map[string][]gitlocal.LineRange, error)
}

// Assess classifies each comment as addressed, open, or unknown. Comments are
// grouped by the commit they were left against so each distinct commit is
// diffed only once. A per-commit diff failure downgrades that group to unknown
// rather than failing the whole assessment.
func Assess(ctx context.Context, src ChangeSource, comments []forge.ReviewComment) []CommentProgress {
	// Group comment indices by the commit they reference, collecting the paths
	// involved so each diff can be scoped.
	type group struct {
		idxs  []int
		paths map[string]struct{}
	}
	byCommit := make(map[string]*group)
	for i, c := range comments {
		g := byCommit[c.CommitID]
		if g == nil {
			g = &group{paths: make(map[string]struct{})}
			byCommit[c.CommitID] = g
		}
		g.idxs = append(g.idxs, i)
		g.paths[c.Path] = struct{}{}
	}

	out := make([]CommentProgress, len(comments))
	for i, c := range comments {
		out[i] = CommentProgress{Comment: c, Status: StatusOpen}
	}

	for commit, g := range byCommit {
		// A missing commit or an unlocated (line 0) comment can't be assessed.
		if commit == "" {
			markUnknown(out, g.idxs)
			continue
		}
		paths := make([]string, 0, len(g.paths))
		for p := range g.paths {
			paths = append(paths, p)
		}
		changed, err := src.ChangedLinesSince(ctx, commit, paths)
		if err != nil {
			markUnknown(out, g.idxs)
			continue
		}
		for _, i := range g.idxs {
			c := comments[i]
			if c.Line <= 0 {
				out[i].Status = StatusUnknown
				continue
			}
			if lineChanged(changed[c.Path], c.Line) {
				out[i].Status = StatusAddressed
			}
		}
	}
	return out
}

func markUnknown(out []CommentProgress, idxs []int) {
	for _, i := range idxs {
		out[i].Status = StatusUnknown
	}
}

func lineChanged(ranges []gitlocal.LineRange, line int) bool {
	for _, r := range ranges {
		if r.Contains(line) {
			return true
		}
	}
	return false
}

// Summary counts comments by status.
type Summary struct {
	Addressed int
	Open      int
	Unknown   int
}

// Summarize tallies a progress slice.
func Summarize(ps []CommentProgress) Summary {
	var s Summary
	for _, p := range ps {
		switch p.Status {
		case StatusAddressed:
			s.Addressed++
		case StatusUnknown:
			s.Unknown++
		default:
			s.Open++
		}
	}
	return s
}
