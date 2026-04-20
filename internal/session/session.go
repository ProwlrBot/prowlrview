package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Session struct {
	Name      string    `json:"name"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func baseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "prowlrview", "sessions")
}

func sessionDir(name string) string {
	return filepath.Join(baseDir(), name)
}

func metaPath(name string) string {
	return filepath.Join(sessionDir(name), "session.json")
}

// ActivePath returns the path to the "current" symlink that points to active session dir.
func ActivePath() string {
	return filepath.Join(baseDir(), "current")
}

// New creates a new session directory and writes metadata.
func New(name, target string) (*Session, error) {
	dir := sessionDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Session{Name: name, Target: target, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(metaPath(name), data, 0o644); err != nil {
		return nil, err
	}
	return s, nil
}

// List returns all sessions sorted by creation time (newest first).
func List() ([]*Session, error) {
	entries, err := os.ReadDir(baseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "current" {
			continue
		}
		data, err := os.ReadFile(metaPath(e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err == nil {
			out = append(out, &s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Switch creates (or updates) a "current" symlink pointing to name's dir.
func Switch(name string) error {
	dir := sessionDir(name)
	if _, err := os.Stat(metaPath(name)); err != nil {
		return fmt.Errorf("session %q not found", name)
	}
	cur := ActivePath()
	_ = os.Remove(cur)
	return os.Symlink(dir, cur)
}

// Active returns the currently active session name (via symlink), or "" if none.
func Active() string {
	cur := ActivePath()
	target, err := os.Readlink(cur)
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// SnapshotPath returns the path where the session's graph snapshot should be saved.
func SnapshotPath(name string) string {
	return filepath.Join(sessionDir(name), "graph.snapshot.jsonl")
}

// PluginDir returns a session-scoped plugin override dir (empty = use global).
func PluginDir(name string) string {
	return filepath.Join(sessionDir(name), "plugins")
}

// Touch updates the session's UpdatedAt timestamp.
func Touch(name string) {
	data, err := os.ReadFile(metaPath(name))
	if err != nil {
		return
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	s.UpdatedAt = time.Now()
	updated, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(metaPath(name), updated, 0o644)
}
