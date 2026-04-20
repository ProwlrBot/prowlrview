package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const pluginsRepo = "https://github.com/ProwlrBot/prowlrview-plugins.git"

// RepoPath returns the local plugins-repo location, cloning it if absent.
// Search order: $PROWLRVIEW_PLUGINS_REPO, ~/prowlrview-plugins, ~/src/prowlrview-plugins, $XDG_CACHE_HOME.
func RepoPath() (string, error) {
	if p := os.Getenv("PROWLRVIEW_PLUGINS_REPO"); p != "" {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, "prowlrview-plugins"),
		filepath.Join(home, "src", "prowlrview-plugins"),
	} {
		if st, err := os.Stat(filepath.Join(c, "categories")); err == nil && st.IsDir() {
			return c, nil
		}
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	dst := filepath.Join(cache, "prowlrview", "plugins-repo")
	if st, err := os.Stat(filepath.Join(dst, "categories")); err == nil && st.IsDir() {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "cloning %s → %s\n", pluginsRepo, dst)
	cmd := exec.Command("git", "clone", "--depth", "1", pluginsRepo, dst)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("clone: %w", err)
	}
	return dst, nil
}

// ThemeDir mirrors UserPluginDir for themes.
func ThemeDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "prowlrview", "themes")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "prowlrview", "themes")
}

type Entry struct {
	Name     string
	Category string
	Source   string // abs path inside repo
	Target   string // abs path under ~/.config
	Kind     string // "plugin" | "theme"
	Enabled  bool
}

// Scan returns every plugin+theme available in the repo, with enabled status.
func Scan() ([]Entry, error) {
	repo, err := RepoPath()
	if err != nil {
		return nil, err
	}
	var out []Entry
	catDir := filepath.Join(repo, "categories")
	cats, err := os.ReadDir(catDir)
	if err != nil {
		return nil, err
	}
	pluginsTgt := UserPluginDir()
	themesTgt := ThemeDir()
	for _, c := range cats {
		if !c.IsDir() {
			continue
		}
		items, _ := os.ReadDir(filepath.Join(catDir, c.Name()))
		for _, it := range items {
			if !it.IsDir() {
				continue
			}
			base := filepath.Join(catDir, c.Name(), it.Name())
			if c.Name() == "themes" {
				src := filepath.Join(base, "theme.toml")
				if !exists(src) {
					continue
				}
				tgt := filepath.Join(themesTgt, it.Name()+".toml")
				out = append(out, Entry{
					Name: it.Name(), Category: c.Name(),
					Source: src, Target: tgt, Kind: "theme",
					Enabled: symlinkMatches(tgt, src),
				})
				continue
			}
			src := filepath.Join(base, "main.lua")
			if !exists(src) {
				continue
			}
			tgt := filepath.Join(pluginsTgt, it.Name()+".lua")
			out = append(out, Entry{
				Name: it.Name(), Category: c.Name(),
				Source: src, Target: tgt, Kind: "plugin",
				Enabled: symlinkMatches(tgt, src),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Install creates the symlink; enables the plugin.
func Install(e Entry) error {
	if err := os.MkdirAll(filepath.Dir(e.Target), 0o755); err != nil {
		return err
	}
	_ = os.Remove(e.Target)
	return os.Symlink(e.Source, e.Target)
}

// Uninstall removes the symlink but leaves the source file in the repo.
func Uninstall(e Entry) error {
	if st, err := os.Lstat(e.Target); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return os.Remove(e.Target)
	}
	return nil
}

// PrintList writes a human-friendly listing to w.
func PrintList(w io.Writer, entries []Entry) {
	cat := ""
	for _, e := range entries {
		if e.Category != cat {
			fmt.Fprintf(w, "\n  %s\n", strings.ToUpper(e.Category))
			cat = e.Category
		}
		flag := "·"
		if e.Enabled {
			flag = "✓"
		}
		fmt.Fprintf(w, "    %s %-24s (%s)\n", flag, e.Name, e.Kind)
	}
}

// ManifestEntry is a single plugin entry from manifest.json.
type ManifestEntry struct {
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
}

// Manifest is the top-level structure of manifest.json.
type Manifest struct {
	Version string          `json:"version"`
	Updated string          `json:"updated"`
	Plugins []ManifestEntry `json:"plugins"`
}

// LoadManifest reads manifest.json from the plugins repo.
func LoadManifest() (*Manifest, error) {
	repo, err := RepoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(repo, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("manifest not found (run: prowlrview plugin update): %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SearchManifest returns entries whose name, category, or tags contain query (case-insensitive).
func SearchManifest(query string) ([]ManifestEntry, error) {
	m, err := LoadManifest()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var out []ManifestEntry
	for _, e := range m.Plugins {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Category), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
			continue
		}
		for _, t := range e.Tags {
			if strings.Contains(strings.ToLower(t), q) {
				out = append(out, e)
				break
			}
		}
	}
	return out, nil
}

// UpdateRepo runs `git pull` on the plugins repo.
func UpdateRepo() error {
	repo, err := RepoPath()
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "-C", repo, "pull", "--ff-only")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func symlinkMatches(link, want string) bool {
	got, err := os.Readlink(link)
	return err == nil && got == want
}
