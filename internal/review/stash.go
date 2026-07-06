package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// StashDir is the per-repo state directory (git-ignored).
const StashDir = ".nitpick"

const stashFile = "stash.json"

// StashEntry records a review comment that Nitpick posted, so it can be
// reviewed or re-posted later (e.g. after a force-push).
type StashEntry struct {
	PR        int       `json:"pr"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Body      string    `json:"body"`
	CommitID  string    `json:"commit_id"`
	CommentID int64     `json:"comment_id"`
	URL       string    `json:"url,omitempty"`
	PostedAt  time.Time `json:"posted_at"`
}

// Stash is the on-disk log of posted comments.
type Stash struct {
	Entries []StashEntry `json:"entries"`
}

// LoadStash reads the stash rooted at dir (the repo root). A missing stash is
// not an error; it returns an empty Stash.
func LoadStash(dir string) (*Stash, error) {
	path := filepath.Join(dir, StashDir, stashFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Stash{}, nil
		}
		return nil, err
	}
	var s Stash
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Add appends an entry to the stash.
func (s *Stash) Add(e StashEntry) {
	s.Entries = append(s.Entries, e)
}

// Save writes the stash under dir/.nitpick/, creating the directory as needed.
func (s *Stash) Save(dir string) error {
	stashPath := filepath.Join(dir, StashDir)
	if err := os.MkdirAll(stashPath, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(stashPath, stashFile), data)
}
