package theme

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// LoadUserThemes reads *.toml files from dir and merges them over builtins.
// Minimal TOML subset: `key = "value"` and `[section]` headers.
func LoadUserThemes(dir string, into map[string]*Theme) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		t, err := parseThemeFile(filepath.Join(dir, e.Name()))
		if err != nil || t == nil || t.Name == "" {
			continue
		}
		into[t.Name] = t
	}
	return nil
}

func parseThemeFile(path string) (*Theme, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	t := &Theme{
		Background: tcell.ColorBlack, Foreground: tcell.ColorWhite,
		Border: tcell.ColorWhite, Accent: tcell.ColorAqua, Title: tcell.ColorFuchsia,
		SevCritical: tcell.ColorRed, SevHigh: tcell.ColorOrange,
		SevMedium: tcell.ColorYellow, SevLow: tcell.ColorGreen, SevInfo: tcell.ColorGray,
	}

	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		applyKV(t, section, key, val)
	}
	return t, sc.Err()
}

func applyKV(t *Theme, section, key, val string) {
	c := parseHex(val)
	switch section {
	case "":
		switch key {
		case "name":
			t.Name = val
		case "background":
			t.Background = c
		case "foreground":
			t.Foreground = c
		case "border":
			t.Border = c
		case "accent":
			t.Accent = c
		case "title":
			t.Title = c
		}
	case "severity":
		switch key {
		case "critical":
			t.SevCritical = c
		case "high":
			t.SevHigh = c
		case "medium":
			t.SevMedium = c
		case "low":
			t.SevLow = c
		case "info":
			t.SevInfo = c
		}
	}
}

func parseHex(s string) tcell.Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return tcell.ColorDefault
	}
	n, err := strconv.ParseInt(s, 16, 32)
	if err != nil {
		return tcell.ColorDefault
	}
	return hex(int32(n))
}

// UserThemeDir returns the canonical path ($XDG_CONFIG_HOME/prowlrview/themes).
func UserThemeDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "prowlrview", "themes")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "prowlrview", "themes")
}
