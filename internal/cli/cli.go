// Package cli implements Nitpick's command-line interface using only the
// standard library's flag package (one FlagSet per subcommand). It builds the
// app service layer and renders results; all forge logic lives elsewhere.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sjrbie/nitpick/internal/app"
	"github.com/sjrbie/nitpick/internal/config"
	"github.com/sjrbie/nitpick/internal/forge"
	"github.com/sjrbie/nitpick/internal/forge/github"
	"github.com/sjrbie/nitpick/internal/gitlocal"
)

const usage = `nitpick - a local-first tool for pull-request review

Usage:
  nitpick <command> [flags]

Commands:
  pr list                List pull requests
  pr view <number>       View a pull request with comments and local progress
  pr comments <number>   List review comments on a pull request
  scrape                 Post "// nit:" comments from your code to the branch's PR
  hook install           Install a pre-commit hook that runs "nitpick scrape"

Run "nitpick <command> -h" for command-specific flags.

Auth:
  Set NITPICK_TOKEN or GITHUB_TOKEN to a token with repo access.
`

// Run executes the CLI with the given args (excluding the program name) and
// returns a process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "pr":
		return runPR(ctx, args[1:], stdout, stderr)
	case "scrape":
		return runScrape(ctx, args[1:], stdout, stderr)
	case "hook":
		return runHook(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "nitpick: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runPR(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "nitpick pr: expected a subcommand (list, view, comments)")
		return 2
	}
	switch args[0] {
	case "list":
		return runPRList(ctx, args[1:], stdout, stderr)
	case "view":
		return runPRView(ctx, args[1:], stdout, stderr)
	case "comments":
		return runPRComments(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "nitpick pr: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runPRList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", "open", "filter by state: open, closed, all")
	asJSON := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	a, code := buildApp(stderr)
	if a == nil {
		return code
	}

	prs, err := a.ListPullRequests(ctx, forge.ListOpts{State: forge.State(*state)})
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	if err := renderPullRequests(stdout, prs, *asJSON); err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	return 0
}

func runPRView(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	number, ok := parseNumberArg(fs.Args(), stderr, "pr view")
	if !ok {
		return 2
	}

	a, code := buildApp(stderr)
	if a == nil {
		return code
	}

	view, err := a.ViewPullRequest(ctx, number)
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	if err := renderPullRequestView(stdout, view, *asJSON); err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	return 0
}

func runPRComments(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr comments", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	number, ok := parseNumberArg(fs.Args(), stderr, "pr comments")
	if !ok {
		return 2
	}

	a, code := buildApp(stderr)
	if a == nil {
		return code
	}

	comments, err := a.ReviewComments(ctx, number)
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	if err := renderComments(stdout, comments, *asJSON); err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	return 0
}

func runScrape(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("scrape", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "show what would be posted without posting or editing files")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	a, code := buildApp(stderr)
	if a == nil {
		return code
	}

	res, err := a.Scrape(ctx, *dryRun)
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	renderScrape(stdout, res)
	if len(res.Failures) > 0 {
		return 1
	}
	return 0
}

func runHook(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "install" {
		fmt.Fprintln(stderr, "nitpick hook: expected subcommand \"install\"")
		return 2
	}
	fs := flag.NewFlagSet("hook install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite an existing pre-commit hook")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	path, err := gitlocal.New(".").HookPath(ctx, "pre-commit")
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(stderr, "nitpick: %s already exists; re-run with --force to overwrite\n", path)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(preCommitHook), 0o755); err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed pre-commit hook at %s\n", path)
	return 0
}

const preCommitHook = `#!/bin/sh
# Installed by nitpick: collect inline "// nit:" markers and post them as
# review comments before the commit is created.
exec nitpick scrape
`

// parseNumberArg reads a required positive PR number from the leftover args.
func parseNumberArg(args []string, stderr io.Writer, cmd string) (int, bool) {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "nitpick %s: expected a pull request number\n", cmd)
		return 0, false
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n <= 0 {
		fmt.Fprintf(stderr, "nitpick %s: invalid pull request number %q\n", cmd, args[0])
		return 0, false
	}
	return n, true
}

// buildApp resolves config and constructs the app. On failure it prints to
// stderr and returns a nil App plus the exit code to use.
func buildApp(stderr io.Writer) (*app.App, int) {
	token, err := config.Token()
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return nil, 1
	}
	remote, err := config.DetectRemote(".")
	if err != nil {
		fmt.Fprintf(stderr, "nitpick: %v\n", err)
		return nil, 1
	}
	provider := github.New(token)
	return app.New(provider, remote, gitlocal.New(".")), 0
}
