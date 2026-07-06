package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/sjrbie/nitpick/internal/app"
	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/track"
)

// renderPullRequests writes prs to w as either JSON or an aligned table.
func renderPullRequests(w io.Writer, prs []forge.PullRequest, asJSON bool) error {
	if asJSON {
		return writeJSON(w, prs)
	}
	if len(prs) == 0 {
		fmt.Fprintln(w, "No pull requests found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tSTATE\tAUTHOR\tTITLE")
	for _, pr := range prs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			strconv.Itoa(pr.Number), pr.State, pr.Author, truncate(pr.Title, 72))
	}
	return tw.Flush()
}

// renderPullRequestView writes a PR header, a progress summary, and the
// assessed review comments.
func renderPullRequestView(w io.Writer, v app.PullRequestView, asJSON bool) error {
	if asJSON {
		return writeJSON(w, v)
	}
	fmt.Fprintf(w, "#%d  %s\n", v.PR.Number, v.PR.Title)
	fmt.Fprintf(w, "%s · %s → %s · by %s\n", v.PR.State, v.PR.HeadRef, v.PR.BaseRef, v.PR.Author)
	if v.PR.URL != "" {
		fmt.Fprintln(w, v.PR.URL)
	}
	fmt.Fprintf(w, "\nFeedback: %d addressed · %d open · %d unknown\n\n",
		v.Summary.Addressed, v.Summary.Open, v.Summary.Unknown)
	writeComments(w, v.Comments)
	return nil
}

// renderComments writes just the assessed review comments.
func renderComments(w io.Writer, comments []track.CommentProgress, asJSON bool) error {
	if asJSON {
		return writeJSON(w, comments)
	}
	if len(comments) == 0 {
		fmt.Fprintln(w, "No review comments.")
		return nil
	}
	writeComments(w, comments)
	return nil
}

// writeComments renders each comment as a status-marked block.
func writeComments(w io.Writer, comments []track.CommentProgress) {
	for _, cp := range comments {
		c := cp.Comment
		loc := c.Path
		if c.Line > 0 {
			loc = fmt.Sprintf("%s:%d", c.Path, c.Line)
		}
		reply := ""
		if c.InReplyTo != 0 {
			reply = " (reply)"
		}
		fmt.Fprintf(w, "%s %s  @%s%s\n", statusMark(cp.Status), loc, c.Author, reply)
		for _, line := range strings.Split(strings.TrimRight(c.Body, "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
		fmt.Fprintln(w)
	}
}

// statusMark returns a short, greppable label for a status.
func statusMark(s track.Status) string {
	switch s {
	case track.StatusAddressed:
		return "[x]"
	case track.StatusOpen:
		return "[ ]"
	default:
		return "[?]"
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
