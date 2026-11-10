//go:build windows

package main

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Тонкий клиент: открывает чат в отдельном окне Edge (или Chrome) без адресной
// строки. Адрес сервера хранится в client.ini рядом с exe и спрашивается один раз.
func main() {
	base := executableDir()
	cfg := filepath.Join(base, "client.ini")

	target := ""
	if len(os.Args) > 1 {
		target = strings.TrimSpace(os.Args[1])
	}
	if target == "" {
		target = readConfig(cfg)
	}
	if target == "" {
		target = askURL()
		if target == "" {
			return
		}
		_ = writeConfig(cfg, target)
	}
	if !validURL(target) {
		msgBox("Адрес должен начинаться с http:// или https://\n\nНапример: http://192.168.1.10:8080")
		return
	}
	if err := openAppWindow(target); err != nil {
		msgBox("Не удалось открыть чат: " + err.Error())
	}
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// openAppWindow запускает браузер в режиме приложения: без вкладок и адресной строки.
func openAppWindow(target string) error {
	for _, name := range []string{"msedge.exe", "chrome.exe"} {
		for _, dir := range browserDirs() {
			full := filepath.Join(dir, name)
			if _, err := os.Stat(full); err == nil {
				return start(full, "--app="+target)
			}
		}
		if full, err := exec.LookPath(name); err == nil {
			return start(full, "--app="+target)
		}
	}
	// Крайний случай: обычный браузер по умолчанию, уже с адресной строкой.
	return start("rundll32", "url.dll,FileProtocolHandler", target)
}

func browserDirs() []string {
	var dirs []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		if root := os.Getenv(env); root != "" {
			dirs = append(dirs,
				filepath.Join(root, "Microsoft", "Edge", "Application"),
				filepath.Join(root, "Google", "Chrome", "Application"))
		}
	}
	return dirs
}

func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func readConfig(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "url=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "url="))
		}
	}
	return ""
}

func writeConfig(path, target string) error {
	return os.WriteFile(path, []byte("url="+target+"\r\n"), 0o644)
}

// askURL показывает простое окно ввода: без него клиент молча ничего бы не делал.
func askURL() string {
	const script = `Add-Type -AssemblyName Microsoft.VisualBasic; ` +
		`[Microsoft.VisualBasic.Interaction]::InputBox('Адрес сервера Офис-чата','Офис-чат','http://localhost:8080')`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func msgBox(text string) {
	script := `Add-Type -AssemblyName Microsoft.VisualBasic; ` +
		`[Microsoft.VisualBasic.Interaction]::MsgBox(` + psQuote(text) + `,0,'Офис-чат')`
	_ = exec.Command("powershell", "-NoProfile", "-Command", script).Run()
}

// psQuote экранирует строку для одинарных кавычек PowerShell.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
