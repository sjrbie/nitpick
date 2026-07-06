// Package app is the orchestration layer between the interface (CLI today, TUI
// later) and the forge/config packages. It holds no I/O formatting so the same
// methods can back either front end.
package app

import (
	"context"
	"errors"
	"sync"

	"github.com/sjrbie/nitpick/internal/config"
	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/gitlocal"
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
