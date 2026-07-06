// Package app is the orchestration layer between the interface (CLI today, TUI
// later) and the forge/config packages. It holds no I/O formatting so the same
// methods can back either front end.
package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sjrbie/nitpick/internal/config"
	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/gitlocal"
	"github.com/sjrbie/nitpick/internal/review"
	"github.com/sjrbie/nitpick/internal/track"
)

// App wires a forge repository handle together with resolved config and the
// local git working tree.
type App struct {
	repo   forge.Repository
	remote config.Remote
	git    *gitlocal.Repo
}

// New constructs an App bound to a specific repository on a forge. git may be
// nil when local-tree features are not needed.
func New(f forge.Forge, remote config.Remote, git *gitlocal.Repo) *App {
	return &App{
		repo:   f.Repo(remote.Owner, remote.Name),
		remote: remote,
		git:    git,
	}
}

// Remote reports the repository this App is bound to.
func (a *App) Remote() config.Remote { return a.remote }

// ListPullRequests collects pull requests matching opts into a slice. The
// underlying forge call streams and follows pagination; this drains it.
func (a *App) ListPullRequests(ctx context.Context, opts forge.ListOpts) ([]forge.PullRequest, error) {
	var out []forge.PullRequest
	for pr, err := range a.repo.ListPullRequests(ctx, opts) {
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// PullRequestView is a pull request together with its review comments and the
// locally-assessed progress on each.
type PullRequestView struct {
	PR       forge.PullRequest
	Comments []track.CommentProgress
	Summary  track.Summary
}

// ViewPullRequest fetches a pull request and its review comments concurrently,
// then assesses local progress on each comment. Network errors from either
// fetch are joined and returned together.
func (a *App) ViewPullRequest(ctx context.Context, number int) (PullRequestView, error) {
	var (
		wg       sync.WaitGroup
		pr       forge.PullRequest
		comments []forge.ReviewComment
		prErr    error
		comErr   error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		pr, prErr = a.repo.GetPullRequest(ctx, number)
	}()
	go func() {
		defer wg.Done()
		comments, comErr = a.repo.ListReviewComments(ctx, number)
	}()
	wg.Wait()

	if err := errors.Join(prErr, comErr); err != nil {
		return PullRequestView{}, err
	}

	var progress []track.CommentProgress
	if a.git != nil {
		progress = track.Assess(ctx, a.git, comments)
	} else {
		progress = make([]track.CommentProgress, len(comments))
		for i, c := range comments {
			progress[i] = track.CommentProgress{Comment: c, Status: track.StatusUnknown}
		}
	}

	return PullRequestView{
		PR:       pr,
		Comments: progress,
		Summary:  track.Summarize(progress),
	}, nil
}

// ScrapeFailure records a marker that could not be posted.
type ScrapeFailure struct {
	Marker review.Marker
	Err    error
}

// ScrapeResult reports what a scrape did (or would do, when DryRun is set).
type ScrapeResult struct {
	PR       forge.PullRequest
	DryRun   bool
	Posted   []review.Marker // markers posted (or, for a dry run, that would be)
	Failures []ScrapeFailure
	Warning  string // non-fatal issue detected (e.g. branch not pushed), dry-run only
}

// shortSHA abbreviates a commit hash for display.
func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Scrape collects "// nit:" markers from the working tree and posts them as
// review comments on the current branch's open pull request. On success each
// marker is stripped from its file and recorded in the stash. With dryRun set,
// it reports what would be posted without touching the forge or the files.
func (a *App) Scrape(ctx context.Context, dryRun bool) (ScrapeResult, error) {
	if a.git == nil {
		return ScrapeResult{}, errors.New("scrape requires a local git working tree")
	}

	pr, err := a.currentBranchPR(ctx)
	if err != nil {
		return ScrapeResult{}, err
	}
	markers, err := review.Collect(ctx, a.git, ".")
	if err != nil {
		return ScrapeResult{}, err
	}
	headSHA, err := a.git.HeadSHA(ctx)
	if err != nil {
		return ScrapeResult{}, err
	}

	res := ScrapeResult{PR: pr, DryRun: dryRun}

	// A review comment can only anchor to a commit that is part of the PR, and
	// its line numbers must match that commit's version of the file. If local
	// HEAD isn't the PR's head, our line math won't match what GitHub has and
	// every post would 422 — stop early with an actionable message. On a dry
	// run we surface it as a warning instead of failing.
	if pr.HeadSHA != "" && headSHA != pr.HeadSHA {
		msg := fmt.Sprintf("local HEAD (%s) is not the head of PR #%d (%s); push your branch so the PR includes your latest commit, then retry",
			shortSHA(headSHA), pr.Number, shortSHA(pr.HeadSHA))
		if !dryRun {
			return ScrapeResult{}, errors.New(msg)
		}
		res.Warning = msg
	}

	if dryRun {
		res.Posted = markers // "would post"
		return res, nil
	}
	if len(markers) == 0 {
		return res, nil
	}

	// Anchor to the PR's head commit (guaranteed to be in the PR); it equals
	// local HEAD after the guard above.
	commitID := pr.HeadSHA
	if commitID == "" {
		commitID = headSHA
	}
	stash, err := review.LoadStash(".")
	if err != nil {
		return ScrapeResult{}, err
	}

	for _, m := range markers {
		c, err := a.repo.CreateReviewComment(ctx, pr.Number, forge.NewComment{
			Path:     m.Path,
			Line:     m.Line,
			Side:     forge.SideRight,
			CommitID: commitID,
			Body:     m.Body,
		})
		if err != nil {
			res.Failures = append(res.Failures, ScrapeFailure{Marker: m, Err: err})
			continue
		}
		res.Posted = append(res.Posted, m)
		stash.Add(review.StashEntry{
			PR:        pr.Number,
			Path:      m.Path,
			Line:      m.Line,
			Body:      m.Body,
			CommitID:  commitID,
			CommentID: c.ID,
			URL:       c.URL,
			PostedAt:  time.Now(),
		})
	}

	// Only mutate the working tree and stash for markers that actually posted.
	if len(res.Posted) > 0 {
		if err := review.StripMarkers(".", res.Posted); err != nil {
			return res, fmt.Errorf("posted %d comment(s) but failed to strip markers: %w", len(res.Posted), err)
		}
		if err := stash.Save("."); err != nil {
			return res, fmt.Errorf("posted %d comment(s) but failed to save stash: %w", len(res.Posted), err)
		}
	}
	return res, nil
}

// currentBranchPR finds the open pull request whose head is the checked-out
// branch, first via the forge's head filter and then by matching head refs.
func (a *App) currentBranchPR(ctx context.Context) (forge.PullRequest, error) {
	branch, err := a.git.CurrentBranch(ctx)
	if err != nil {
		return forge.PullRequest{}, err
	}
	head := a.remote.Owner + ":" + branch
	prs, err := a.ListPullRequests(ctx, forge.ListOpts{State: forge.StateOpen, Head: head})
	if err != nil {
		return forge.PullRequest{}, err
	}
	if len(prs) > 0 {
		return prs[0], nil
	}
	// Fallback for forks or providers that ignore the head filter.
	prs, err = a.ListPullRequests(ctx, forge.ListOpts{State: forge.StateOpen})
	if err != nil {
		return forge.PullRequest{}, err
	}
	for _, pr := range prs {
		if pr.HeadRef == branch {
			return pr, nil
		}
	}
	return forge.PullRequest{}, fmt.Errorf("no open pull request found for branch %q", branch)
}

// ReviewComments returns just the assessed review comments for a pull request.
func (a *App) ReviewComments(ctx context.Context, number int) ([]track.CommentProgress, error) {
	comments, err := a.repo.ListReviewComments(ctx, number)
	if err != nil {
		return nil, err
	}
	if a.git == nil {
		out := make([]track.CommentProgress, len(comments))
		for i, c := range comments {
			out[i] = track.CommentProgress{Comment: c, Status: track.StatusUnknown}
		}
		return out, nil
	}
	return track.Assess(ctx, a.git, comments), nil
}
