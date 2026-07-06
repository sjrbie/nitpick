package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/sjrbie/nitpick/internal/forge"
)

// newTestRepo builds a repo pointed at srv with fast, deterministic timing.
func newTestRepo(srv *httptest.Server) *repo {
	c := newClient("test-token", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	return &repo{c: c, owner: "o", name: "r"}
}

func TestListPullRequestsPagination(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		page := r.URL.Query().Get("page")
		switch page {
		case "", "1":
			// Point to page 2 via a rel="next" Link header.
			next := fmt.Sprintf("<http://%s/repos/o/r/pulls?page=2>; rel=\"next\"", r.Host)
			w.Header().Set("Link", next)
			fmt.Fprint(w, `[{"number":1,"title":"first","state":"open","user":{"login":"alice"}}]`)
		case "2":
			fmt.Fprint(w, `[{"number":2,"title":"second","state":"open","user":{"login":"bob"}}]`)
		default:
			t.Errorf("unexpected page %q", page)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	r := newTestRepo(srv)
	prs, err := drain(r.ListPullRequests(context.Background(), forge.ListOpts{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d PRs across pages, want 2", len(prs))
	}
	if prs[0].Number != 1 || prs[1].Number != 2 {
		t.Errorf("wrong PR numbers: %+v", prs)
	}
	if prs[0].Author != "alice" {
		t.Errorf("author mapping failed: %q", prs[0].Author)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth header = %q, want Bearer test-token", gotAuth)
	}
}

func TestErrorDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	r := newTestRepo(srv)
	_, err := r.GetPullRequest(context.Background(), 99)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 || apiErr.Message != "Not Found" {
		t.Errorf("got %+v", apiErr)
	}
	if !NotFound(err) {
		t.Error("NotFound should report true for a 404")
	}
}

func TestRateLimitRetry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"rate limited"}`)
			return
		}
		fmt.Fprint(w, `{"number":7,"title":"ok","state":"open"}`)
	}))
	defer srv.Close()

	r := newTestRepo(srv)
	// Replace sleep with an instant no-op so the test doesn't actually wait.
	var slept time.Duration
	r.c.sleep = func(_ context.Context, d time.Duration) error { slept = d; return nil }

	pr, err := r.GetPullRequest(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if pr.Number != 7 {
		t.Errorf("got PR %d, want 7", pr.Number)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (429 then success), got %d", calls)
	}
	if slept <= 0 {
		t.Error("expected a non-zero rate-limit wait")
	}
}

// drain collects an iterator into a slice, stopping at the first error.
func drain(seq func(func(forge.PullRequest, error) bool)) ([]forge.PullRequest, error) {
	var out []forge.PullRequest
	var ferr error
	seq(func(pr forge.PullRequest, err error) bool {
		if err != nil {
			ferr = err
			return false
		}
		out = append(out, pr)
		return true
	})
	return out, ferr
}
