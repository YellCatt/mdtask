package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// cmdInstall 把 daemon 装进开机启动项：Windows 启动文件夹 .vbs / Linux systemd --user
func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Parse(args)
	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	exe, _ = filepath.Abs(exe)
	data, _ := filepath.Abs(st.path)

	// 启动参数里带上配置文件，避免开机启动时工作目录不同导致找不到
	runArgs := fmt.Sprintf(`-file "%s"`, data)
	if cfgPathUsed != "" {
		runArgs += fmt.Sprintf(` -config "%s"`, cfgPathUsed)
	}

	switch runtime.GOOS {
	case "windows":
		dir, err := os.UserConfigDir() // %APPDATA%
		if err != nil {
			fatal(err)
		}
		startup := filepath.Join(dir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
		if err := os.MkdirAll(startup, 0o755); err != nil {
			fatal(err)
		}
		vbs := filepath.Join(startup, "mdtask.vbs")
		content := fmt.Sprintf(
			"Set ws = CreateObject(\"WScript.Shell\")\n"+
				"ws.CurrentDirectory = \"%s\"\n"+
				"ws.Run \"\"\"%s\"\" %s daemon\", 0, False\n",
			filepath.Dir(exe), exe, runArgs)
		if err := os.WriteFile(vbs, []byte(content), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("已安装开机启动: %s\n", vbs)
		fmt.Println("注销再登录或重启后生效；现在也可以直接双击它启动。")

	case "linux", "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			fatal(err)
		}
		dir := filepath.Join(home, ".config", "systemd", "user")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fatal(err)
		}
		unit := fmt.Sprintf(`[Unit]
Description=MDTask daemon
After=default.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s %s daemon
Restart=always
RestartSec=30

[Install]
WantedBy=default.target
`, filepath.Dir(data), exe, runArgs)
		path := filepath.Join(dir, "mdtask.service")
		if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("已写入 %s\n", path)
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err == nil {
			if err := exec.Command("systemctl", "--user", "enable", "--now", "mdtask").Run(); err == nil {
				fmt.Println("已设置开机启动并立即运行：systemctl --user status mdtask 可查看状态")
				return
			}
		}
		fmt.Println("请手动执行：systemctl --user daemon-reload && systemctl --user enable --now mdtask")

	default:
		fatal("暂不支持自动安装，请手动把 `mdtask daemon` 加到开机启动项")
	}
}

func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	fs.Parse(args)
	switch runtime.GOOS {
	case "windows":
		dir, err := os.UserConfigDir()
		if err != nil {
			fatal(err)
		}
		p := filepath.Join(dir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "mdtask.vbs")
		if err := os.Remove(p); err != nil {
			fatal(err)
		}
		fmt.Println("已移除开机启动:", p)
	case "linux", "darwin":
		exec.Command("systemctl", "--user", "disable", "--now", "mdtask").Run()
		home, _ := os.UserHomeDir()
		p := filepath.Join(home, ".config", "systemd", "user", "mdtask.service")
		if err := os.Remove(p); err != nil {
			fatal(err)
		}
		fmt.Println("已移除:", p)
	default:
		fatal("暂不支持")
	}
}
