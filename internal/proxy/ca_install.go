package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CAPath is the public accessor for the CA cert path.
func CAPath() string { return caPath() }

// EnsureCA generates the CA + key if missing. Returns the cert path.
func EnsureCA() (string, error) {
	if _, _, err := loadOrCreateCA(); err != nil {
		return "", err
	}
	return caPath(), nil
}

// ExportTo copies the CA cert to dest. If dest is a dir, writes "prowlrview-ca.crt".
// Resolves common WSL→Windows paths if dest starts with "win:" (e.g. "win:Downloads").
func ExportTo(dest string) (string, error) {
	src, err := EnsureCA()
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(dest, "win:") {
		win, err := wslWindowsPath(strings.TrimPrefix(dest, "win:"))
		if err != nil {
			return "", err
		}
		dest = win
	}
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		dest = filepath.Join(dest, "prowlrview-ca.crt")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return dest, nil
}

// wslWindowsPath maps a Windows-relative location ("Downloads", "Desktop", "")
// to /mnt/c/Users/<USER>/<rel> when running under WSL.
func wslWindowsPath(rel string) (string, error) {
	if !isWSL() {
		return "", fmt.Errorf("not running under WSL — use a normal path instead of win:")
	}
	user, err := windowsUser()
	if err != nil {
		return "", err
	}
	base := filepath.Join("/mnt/c/Users", user)
	if rel == "" {
		return filepath.Join(base, "Downloads", "prowlrview-ca.crt"), nil
	}
	return filepath.Join(base, rel, "prowlrview-ca.crt"), nil
}

func isWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, _ := os.ReadFile("/proc/version")
	return strings.Contains(strings.ToLower(string(b)), "microsoft")
}

func windowsUser() (string, error) {
	// Prefer the writable, non-default user dir under /mnt/c/Users
	entries, _ := os.ReadDir("/mnt/c/Users")
	skip := map[string]bool{"Public": true, "Default": true, "All Users": true, "DevToolsUser": true, "WDAGUtilityAccount": true, "desktop.ini": true}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || skip[n] || strings.HasPrefix(n, "Default") {
			continue
		}
		probe := filepath.Join("/mnt/c/Users", n, ".prowlrview-write-test")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return n, nil
		}
	}
	if out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", "$env:USERNAME").Output(); err == nil {
		s := strings.TrimSpace(strings.Trim(string(out), "\r\n"))
		if s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("could not detect Windows user; pass an absolute path instead")
}

// Instructions returns platform-specific trust steps for the user.
func Instructions(certPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CA cert: %s\n\n", certPath)
	if isWSL() {
		fmt.Fprintln(&b, "WSL detected — to trust on Windows host:")
		fmt.Fprintln(&b, "  prowlrview ca export win:Downloads      # copies to C:\\Users\\<you>\\Downloads")
		fmt.Fprintln(&b, "  Then on Windows: double-click prowlrview-ca.crt → Install Certificate →")
		fmt.Fprintln(&b, "    Local Machine → \"Place in: Trusted Root Certification Authorities\"")
		fmt.Fprintln(&b, "  OR run in admin PowerShell:")
		fmt.Fprintln(&b, `    certutil -addstore -f "ROOT" "$env:USERPROFILE\Downloads\prowlrview-ca.crt"`)
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Chrome (no system trust needed):  prowlrview chrome https://example.com")
		return b.String()
	}
	switch runtime.GOOS {
	case "linux":
		fmt.Fprintln(&b, "Linux — system-wide trust:")
		fmt.Fprintln(&b, "  sudo cp", certPath, "/usr/local/share/ca-certificates/prowlrview.crt")
		fmt.Fprintln(&b, "  sudo update-ca-certificates")
		fmt.Fprintln(&b, "Firefox stores certs separately — Settings → Privacy & Security → Certificates → Import.")
	case "darwin":
		fmt.Fprintln(&b, "macOS — add to system keychain:")
		fmt.Fprintln(&b, "  sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain", certPath)
	case "windows":
		fmt.Fprintln(&b, "Windows (admin PowerShell):")
		fmt.Fprintf(&b, "  certutil -addstore -f \"ROOT\" \"%s\"\n", certPath)
	}
	fmt.Fprintln(&b, "\nChrome (no system trust needed):  prowlrview chrome https://example.com")
	return b.String()
}

// LaunchChrome opens an isolated Chrome profile pointed at the proxy, with
// cert errors ignored (since we own the MITM CA). Safe — uses --user-data-dir
// so the user's normal Chrome profile is untouched.
func LaunchChrome(proxyAddr, url string) error {
	if proxyAddr == "" {
		proxyAddr = "127.0.0.1:8888"
	}
	if strings.HasPrefix(proxyAddr, ":") {
		proxyAddr = "127.0.0.1" + proxyAddr
	}
	if url == "" {
		url = "https://example.com"
	}
	profile, err := os.MkdirTemp("", "prowlrview-chrome-*")
	if err != nil {
		return err
	}
	bin, err := findChrome()
	if err != nil {
		return err
	}
	args := []string{
		"--user-data-dir=" + profile,
		"--proxy-server=http://" + proxyAddr,
		"--proxy-bypass-list=127.0.0.1;localhost;<-loopback>",
		"--ignore-certificate-errors",
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		url,
	}
	fmt.Fprintf(os.Stderr, "launching %s → proxy %s (isolated profile %s)\n", bin, proxyAddr, profile)
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Start()
}

func findChrome() (string, error) {
	candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser"}
	if isWSL() {
		candidates = append(candidates,
			"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
			"/mnt/c/Program Files (x86)/Google/Chrome/Application/chrome.exe",
			"/mnt/c/Program Files/BraveSoftware/Brave-Browser/Application/brave.exe",
		)
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		)
	case "windows":
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("chrome/chromium not found — install one or pass --bin")
}
