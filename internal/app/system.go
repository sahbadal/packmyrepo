package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type clipCmd struct {
	name string
	args []string
}

func copyCommands() []clipCmd {
	switch runtime.GOOS {
	case "darwin":
		return []clipCmd{{"pbcopy", nil}}
	case "windows":
		return []clipCmd{{"powershell", []string{"-NoProfile", "-Command", "[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())"}}, {"clip", nil}}
	}
	return []clipCmd{{"wl-copy", nil}, {"xclip", []string{"-selection", "clipboard"}}, {"xsel", []string{"--clipboard", "--input"}}}
}

func pasteCommands() []clipCmd {
	switch runtime.GOOS {
	case "darwin":
		return []clipCmd{{"pbpaste", nil}}
	case "windows":
		return []clipCmd{{"powershell", []string{"-NoProfile", "-Command", "[Console]::OutputEncoding=[Text.Encoding]::UTF8; Get-Clipboard -Raw"}}}
	}
	return []clipCmd{{"wl-paste", []string{"--no-newline"}}, {"xclip", []string{"-selection", "clipboard", "-o"}}, {"xsel", []string{"--clipboard", "--output"}}}
}

func copyToClipboard(text string) error {
	for _, c := range copyCommands() {
		if _, err := exec.LookPath(c.name); err != nil {
			continue
		}
		cmd := exec.Command(c.name, c.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return errors.New("no clipboard tool found (install wl-clipboard, xclip or xsel on Linux)")
}

func readClipboard() (string, error) {
	for _, c := range pasteCommands() {
		if _, err := exec.LookPath(c.name); err != nil {
			continue
		}
		var out bytes.Buffer
		cmd := exec.Command(c.name, c.args...)
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			return out.String(), nil
		}
	}
	return "", errors.New("could not read the clipboard")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ---- terminal colors ----

var useColor = func() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}()

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func red(s string) string    { return paint("31", s) }
func cyan(s string) string   { return paint("36", s) }

func stdinIsPiped() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice == 0
}
