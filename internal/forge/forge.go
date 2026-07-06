// Package forge defines the provider-agnostic model and interfaces for a code
// forge (GitHub, GitLab, ...). Concrete providers live in subpackages such as
// forge/github and are the only place that speaks a specific forge's API. The
// rest of Nitpick depends only on the types and interfaces declared here.
package forge

import (
	"context"
	"iter"
	"time"
)

// State is the lifecycle state of a pull/merge request.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateMerged State = "merged"
	StateAll    State = "all"
)

// Side identifies which side of a diff a review comment is anchored to.
type Side string

const (
	// SideRight anchors to the new (added) version of a line. This is the
	// common case for review feedback.
	SideRight Side = "RIGHT"
	// SideLeft anchors to the old (deleted) version of a line.
	SideLeft Side = "LEFT"
)

// PullRequest is a provider-agnostic view of a pull/merge request.
type PullRequest struct {
	Number    int
	Title     string
	State     State
	Author    string
	HeadRef   string // source branch name
	HeadSHA   string // tip commit of the source branch
	BaseRef   string // target branch name
	URL       string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReviewComment is a single inline review comment on a pull request.
type ReviewComment struct {
	ID        int64
	Path      string // repo-relative file path
	Line      int    // line in the file the comment is anchored to (0 if outdated)
	Side      Side
	DiffHunk  string
	Body      string
	Author    string
	CommitID  string // commit the comment was left against
	InReplyTo int64  // 0 if this comment starts a thread
	URL       string
	CreatedAt time.Time
}

// NewComment describes an inline review comment to be created.
type NewComment struct {
	Path     string
	Line     int
	Side     Side
	CommitID string
	Body     string
}

// ListOpts filters and paginates a pull-request listing.
type ListOpts struct {
	State   State  // defaults to StateOpen when empty
	Head    string // filter by head ref, e.g. "owner:branch"; empty for no filter
	PerPage int    // provider default when 0
}

// Forge is a code-hosting provider. It is a factory for repository handles so
// that a single authenticated provider can serve many repositories.
type Forge interface {
	// Repo returns a handle to a specific repository on this forge.
	Repo(owner, name string) Repository
}

// Repository is the set of operations Nitpick performs against one repository.
// Adding a new forge (GitLab, etc.) means implementing this interface.
type Repository interface {
	// ListPullRequests streams pull requests matching opts. The iterator
	// yields either a PullRequest or a non-nil error; on error the caller
	// should stop iterating.
	ListPullRequests(ctx context.Context, opts ListOpts) iter.Seq2[PullRequest, error]

	// GetPullRequest fetches a single pull request by number.
	GetPullRequest(ctx context.Context, number int) (PullRequest, error)

	// ListReviewComments returns all inline review comments on a pull request.
	ListReviewComments(ctx context.Context, number int) ([]ReviewComment, error)

	// CreateReviewComment posts a new inline review comment.
	CreateReviewComment(ctx context.Context, number int, c NewComment) (ReviewComment, error)

	// ReplyToReviewComment posts a reply within an existing comment thread.
	ReplyToReviewComment(ctx context.Context, number int, inReplyTo int64, body string) (ReviewComment, error)
}
