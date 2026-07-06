// Package config resolves the settings Nitpick needs to talk to a forge: an
// auth token and the owner/name/host of the repository in the working tree.
package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Remote identifies a repository on a forge.
type Remote struct {
	Host  string // e.g. github.com
	Owner string
	Name  string
}

// Config is the resolved runtime configuration.
type Config struct {
	Token  string
	Remote Remote
}

// tokenEnvVars are consulted in order; the first non-empty wins.
var tokenEnvVars = []string{"NITPICK_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"}

// Token resolves an auth token from the environment, then from the user config
// file (~/.config/nitpick/config as KEY=VALUE lines with a "token" key).
func Token() (string, error) {
	for _, k := range tokenEnvVars {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v, nil
		}
	}
	if v, err := tokenFromFile(); err != nil {
		return "", err
	} else if v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no token found: set NITPICK_TOKEN or GITHUB_TOKEN, or add 'token=...' to %s", userConfigPath())
}

func userConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "nitpick", "config")
	}
	return filepath.Join("~", ".config", "nitpick", "config")
}

func tokenFromFile() (string, error) {
	path := userConfigPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "token" {
			return strings.TrimSpace(val), nil
		}
	}
	return "", sc.Err()
}

// DetectRemote derives the forge Remote from the origin URL of the git repo
// containing dir. It shells out to git so there is no VCS-library dependency.
func DetectRemote(dir string) (Remote, error) {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return Remote{}, fmt.Errorf("could not read git origin remote (is this a git repo with an 'origin'?): %w", err)
	}
	return ParseRemoteURL(strings.TrimSpace(string(out)))
}

// ParseRemoteURL parses a git remote URL in either SSH or HTTPS form into a
// Remote. Supported shapes:
//
//	git@github.com:owner/name.git
//	ssh://git@github.com/owner/name.git
//	https://github.com/owner/name.git
//	https://github.com/owner/name
func ParseRemoteURL(raw string) (Remote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Remote{}, fmt.Errorf("empty remote URL")
	}

	var host, path string
	switch {
	case strings.HasPrefix(raw, "git@"):
		// git@host:owner/name(.git)
		rest := strings.TrimPrefix(raw, "git@")
		h, p, ok := strings.Cut(rest, ":")
		if !ok {
			return Remote{}, fmt.Errorf("malformed scp-style remote %q", raw)
		}
		host, path = h, p
	case strings.Contains(raw, "://"):
		// scheme://[user@]host/owner/name(.git)
		_, rest, _ := strings.Cut(raw, "://")
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		h, p, ok := strings.Cut(rest, "/")
		if !ok {
			return Remote{}, fmt.Errorf("malformed remote URL %q", raw)
		}
		host, path = h, p
	default:
		return Remote{}, fmt.Errorf("unrecognized remote URL %q", raw)
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" {
		return Remote{}, fmt.Errorf("could not extract owner/name from remote %q", raw)
	}
	// Guard against deeper paths (e.g. GitLab subgroups) collapsing silently:
	// keep only the final two segments as name may contain no slash here since
	// Cut split on the first. Re-split to take the last two.
	if i := strings.LastIndex(name, "/"); i >= 0 {
		owner = name[:i]
		name = name[i+1:]
		if j := strings.LastIndex(owner, "/"); j >= 0 {
			owner = owner[j+1:]
		}
	}
	return Remote{Host: host, Owner: owner, Name: name}, nil
}
