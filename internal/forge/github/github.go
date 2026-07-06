package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/url"
	"strconv"
	"time"

	"github.com/sjrbie/nitpick/internal/forge"
)

// Provider is a GitHub forge.Forge backed by an authenticated REST client.
type Provider struct {
	c *client
}

// New returns a GitHub Provider authenticated with token. Pass options such as
// WithBaseURL for GitHub Enterprise.
func New(token string, opts ...Option) *Provider {
	return &Provider{c: newClient(token, opts...)}
}

// Repo implements forge.Forge.
func (p *Provider) Repo(owner, name string) forge.Repository {
	return &repo{c: p.c, owner: owner, name: name}
}

// repo implements forge.Repository for a single owner/name.
type repo struct {
	c     *client
	owner string
	name  string
}

func (r *repo) base() string {
	return fmt.Sprintf("/repos/%s/%s", url.PathEscape(r.owner), url.PathEscape(r.name))
}

// --- wire types (GitHub JSON payloads) ---

type ghUser struct {
	Login string `json:"login"`
}

type ghRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type ghPull struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Merged    bool      `json:"merged"`
	User      ghUser    `json:"user"`
	Head      ghRef     `json:"head"`
	Base      ghRef     `json:"base"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (g ghPull) toDomain() forge.PullRequest {
	state := forge.State(g.State)
	if g.Merged {
		state = forge.StateMerged
	}
	return forge.PullRequest{
		Number:    g.Number,
		Title:     g.Title,
		State:     state,
		Author:    g.User.Login,
		HeadRef:   g.Head.Ref,
		HeadSHA:   g.Head.SHA,
		BaseRef:   g.Base.Ref,
		URL:       g.HTMLURL,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	}
}

type ghComment struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Side      string    `json:"side"`
	DiffHunk  string    `json:"diff_hunk"`
	Body      string    `json:"body"`
	User      ghUser    `json:"user"`
	CommitID  string    `json:"commit_id"`
	InReplyTo int64     `json:"in_reply_to_id"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

func (g ghComment) toDomain() forge.ReviewComment {
	side := forge.Side(g.Side)
	if side == "" {
		side = forge.SideRight
	}
	return forge.ReviewComment{
		ID:        g.ID,
		Path:      g.Path,
		Line:      g.Line,
		Side:      side,
		DiffHunk:  g.DiffHunk,
		Body:      g.Body,
		Author:    g.User.Login,
		CommitID:  g.CommitID,
		InReplyTo: g.InReplyTo,
		URL:       g.HTMLURL,
		CreatedAt: g.CreatedAt,
	}
}

// --- Repository implementation ---

// ListPullRequests streams pull requests, following pagination Link headers.
func (r *repo) ListPullRequests(ctx context.Context, opts forge.ListOpts) iter.Seq2[forge.PullRequest, error] {
	state := opts.State
	if state == "" {
		state = forge.StateOpen
	}
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 30
	}
	q := url.Values{}
	q.Set("state", string(state))
	q.Set("per_page", strconv.Itoa(perPage))
	if opts.Head != "" {
		q.Set("head", opts.Head)
	}
	first := r.base() + "/pulls?" + q.Encode()

	return func(yield func(forge.PullRequest, error) bool) {
		path := first
		for path != "" {
			var page []ghPull
			hdr, err := r.c.do(ctx, "GET", path, nil, &page)
			if err != nil {
				yield(forge.PullRequest{}, err)
				return
			}
			for _, g := range page {
				if !yield(g.toDomain(), nil) {
					return
				}
			}
			path = r.c.nextPageURL(hdr)
		}
	}
}

// GetPullRequest fetches a single pull request.
func (r *repo) GetPullRequest(ctx context.Context, number int) (forge.PullRequest, error) {
	var g ghPull
	path := fmt.Sprintf("%s/pulls/%d", r.base(), number)
	if _, err := r.c.do(ctx, "GET", path, nil, &g); err != nil {
		return forge.PullRequest{}, err
	}
	return g.toDomain(), nil
}

// ListReviewComments returns all inline review comments, following pagination.
func (r *repo) ListReviewComments(ctx context.Context, number int) ([]forge.ReviewComment, error) {
	path := fmt.Sprintf("%s/pulls/%d/comments?per_page=100", r.base(), number)
	var out []forge.ReviewComment
	for path != "" {
		var page []ghComment
		hdr, err := r.c.do(ctx, "GET", path, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, g := range page {
			out = append(out, g.toDomain())
		}
		path = r.c.nextPageURL(hdr)
	}
	return out, nil
}

// CreateReviewComment posts a new inline review comment.
func (r *repo) CreateReviewComment(ctx context.Context, number int, c forge.NewComment) (forge.ReviewComment, error) {
	side := c.Side
	if side == "" {
		side = forge.SideRight
	}
	payload := map[string]any{
		"body":      c.Body,
		"commit_id": c.CommitID,
		"path":      c.Path,
		"line":      c.Line,
		"side":      string(side),
	}
	return r.postComment(ctx, number, payload)
}

// ReplyToReviewComment posts a reply within an existing thread.
func (r *repo) ReplyToReviewComment(ctx context.Context, number int, inReplyTo int64, body string) (forge.ReviewComment, error) {
	payload := map[string]any{
		"body":           body,
		"in_reply_to_id": inReplyTo,
	}
	return r.postComment(ctx, number, payload)
}

func (r *repo) postComment(ctx context.Context, number int, payload map[string]any) (forge.ReviewComment, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return forge.ReviewComment{}, err
	}
	path := fmt.Sprintf("%s/pulls/%d/comments", r.base(), number)
	var g ghComment
	if _, err := r.c.do(ctx, "POST", path, bytes.NewReader(buf), &g); err != nil {
		return forge.ReviewComment{}, err
	}
	return g.toDomain(), nil
}

// NotFound reports whether err is an API 404.
func NotFound(err error) bool {
	if e, ok := asAPIError(err); ok {
		return e.StatusCode == 404
	}
	return false
}
