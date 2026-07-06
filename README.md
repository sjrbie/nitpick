# Nitpick

**Review pull requests without leaving your terminal — or your code.**

Nitpick is a small, fast CLI for working with pull-request review. It brings the
review conversation to where you already are: your editor and your local branch.
Read the feedback on your PR, see at a glance which comments you've already
addressed, and (soon) write replies as ordinary `// nit:` comments right in your
source — Nitpick collects them and posts them for you before you commit.

Built in Go with **almost no dependencies** (standard library only, so far) and
designed so support for other forges — GitLab, Gitea, … — can slot in behind a
single interface.

> **Status:** early days. Reading and progress-tracking work today (GitHub).
> The inline-comment round-trip is next. See [Roadmap](#roadmap).

---

## Why Nitpick?

Code review usually means bouncing between your editor and a browser tab. Nitpick
flips that around:

- **Read PRs where you work.** List and view pull requests and their inline
  review comments from the command line.
- **Know what's left.** Nitpick diffs each comment against your local branch and
  tells you whether the line it points to has been **addressed**, is still
  **open**, or **can't be determined** — a fast, honest progress hint.
- **Reply in code _(coming soon)_.** Drop a `// nit:` note on the relevant line,
  and a pre-commit scraper turns it into a real review comment on the PR.

---

## Install

Nitpick needs [Go 1.26+](https://go.dev/dl/) and `git` on your `PATH`.

```sh
go install github.com/sjrbie/nitpick/cmd/nitpick@latest
```

Or build from a clone:

```sh
git clone https://github.com/sjrbie/nitpick
cd nitpick
go build -o nitpick ./cmd/nitpick
```

---

## Authentication

Nitpick talks to GitHub with a personal access token. Set one of these
environment variables (checked in order):

```sh
export NITPICK_TOKEN=ghp_xxx      # Nitpick-specific, wins if set
export GITHUB_TOKEN=ghp_xxx       # also respected
export GH_TOKEN=ghp_xxx           # also respected
```

…or drop a line in `~/.config/nitpick/config` (or the OS-appropriate config dir):

```ini
token = ghp_xxx
```

The token needs read access to the repositories you review (and, once the
comment round-trip lands, permission to write pull-request review comments).

Nitpick figures out which repository you're in from your `origin` remote, so run
it from inside a git checkout.

---

## Usage

```sh
# List open pull requests (add --state closed|all)
nitpick pr list

# View a PR: metadata, a progress summary, and every review comment
nitpick pr view 42

# Just the review comments for a PR
nitpick pr comments 42
```

Every command supports `--json` for scripting:

```sh
nitpick pr list --json | jq '.[].title'
```

### Reading the progress markers

`pr view` and `pr comments` tag each comment with its assessed state:

```
[x] internal/auth/session.go:88  @reviewer
    nil check missing here

[ ] internal/auth/session.go:120  @reviewer
    this error should be wrapped

[?] internal/auth/old.go:12  @reviewer
    (comment is outdated or the commit isn't checked out locally)
```

| Marker | Meaning                                                            |
| ------ | ----------------------------------------------------------------- |
| `[x]`  | **Addressed** — the commented line changed on your local branch.  |
| `[ ]`  | **Open** — the line is unchanged since the comment.               |
| `[?]`  | **Unknown** — can't tell (outdated comment, or commit not local). |

It's a heuristic meant to guide your attention — not a guarantee that a comment
is truly resolved.

---

## Roadmap

- [x] **Read** — list/view PRs and their review comments (GitHub).
- [x] **Track** — addressed/open/unknown progress from your local branch.
- [ ] **Round-trip** — write `// nit:` comments in code; scrape and post them on
      commit; stash them locally for reuse.
- [ ] **Extensibility** — a second forge (GitLab) to prove the interface; an
      optional interactive TUI over the same core.

---

## Design notes

- **Standard library first.** No CLI framework, no HTTP client library, no VCS
  bindings — just `net/http`, `flag`, and `os/exec`. Fewer dependencies, smaller
  attack surface, faster builds.
- **Pluggable forges.** Everything GitHub-specific lives behind the
  `forge.Forge` / `forge.Repository` interfaces. Adding a provider means
  implementing that one seam.
- **A service layer, not just a CLI.** Orchestration lives in `internal/app`, so
  a future TUI can reuse it wholesale.
- **Concurrent where it counts.** Independent fetches run in parallel with
  goroutines; shared state is mutex-guarded.

---

## Contributing

Issues and pull requests are welcome. Before opening a PR:

```sh
go build ./...
go vet ./...
go test ./...        # add -race once you have a C toolchain available
gofmt -l .           # should print nothing
```

---

## License

[MIT](./LICENSE) © 2026 Sean Breen
