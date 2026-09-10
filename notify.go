package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// notify 弹一条系统通知：Windows 托盘气泡 / Linux notify-send / macOS osascript
func notify(title, text string) {
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
		logf("系统通知发送失败（不影响日报文件）: %v", err)
	}
}

// psQuote 转义后放进 PowerShell 单引号字符串
func psQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\n", "`n")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// openFile 用系统默认程序打开
func openFile(path string) {
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
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}
