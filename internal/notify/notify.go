package notify

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func Notify(title, text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		script := fmt.Sprintf(
			"Add-Type -AssemblyName System.Windows.Forms\n"+
				"$n=New-Object System.Windows.Forms.NotifyIcon\n"+
				"$n.Icon=[System.Drawing.SystemIcons]::Information\n"+
				"$n.BalloonTipTitle='%s'\n"+
				"$n.BalloonTipText='%s'\n"+
				"$n.Visible=$true\n"+
				"$n.ShowBalloonTip(20000)\n"+
				"Start-Sleep -Seconds 6\n"+
				"$n.Dispose()\n",
			psQuote(title), psQuote(text))
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	case "darwin":
		cmd = exec.Command("osascript", "-e",
			fmt.Sprintf("display notification %q with title %q", text, title))
	default:
		cmd = exec.Command("notify-send", "-a", "MDTask", "-t", "20000", title, text)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "系统通知发送失败: %v\n", err)
	}
}

func psQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\n", "`n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

func OpenFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}