package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mdtask/internal/app"
	"mdtask/internal/config"
	"mdtask/internal/status"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func main() {
	file, configPath, noBackup, rest := splitArgs(os.Args[1:])

	cfg, _, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	if file == "" {
		file = os.Getenv("MDTASK_FILE")
	}
	if file == "" {
		file = cfg.File
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		fatal(err)
	}

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup)

	if err := st.Load(); err != nil {
		fatal(fmt.Errorf("打开 %s 失败: %w", abs, err))
	}

	cmd := "ls"
	args := rest
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		cmd, args = rest[0], rest[1:]
	}

	var runErr error
	switch cmd {
	case "ls", "list", "l":
		runErr = app.CmdList(st, cfg, args)
	case "add", "new", "a":
		runErr = app.CmdAdd(st, cfg, args)
	case "show", "s":
		runErr = app.CmdShow(st, cfg, args)
	case "edit", "e":
		runErr = app.CmdEdit(st, cfg, args)
	case "mark", "m", "st":
		runErr = app.CmdMark(st, cfg, args)
	case "done":
		runErr = app.CmdSetStatus(st, cfg, status.Done, args)
	case "doing":
		runErr = app.CmdSetStatus(st, cfg, status.Doing, args)
	case "hold":
		runErr = app.CmdSetStatus(st, cfg, status.Hold, args)
	case "cancel", "drop":
		runErr = app.CmdSetStatus(st, cfg, status.Cancel, args)
	case "todo":
		runErr = app.CmdSetStatus(st, cfg, "", args)
	case "archive", "arch":
		runErr = app.CmdArchive(st, cfg, args)
	case "rm", "del", "remove":
		runErr = app.CmdRemove(st, cfg, args)
	case "daemon", "d":
		runErr = app.CmdDaemon(st, cfg, args)
	case "report", "r", "rp":
		runErr = app.CmdReport(st, cfg, args)
	case "mail", "ipmail":
		runErr = app.CmdMail(st, cfg, args)
	case "install":
		runErr = app.CmdInstall(st, cfg, args)
	case "uninstall":
		runErr = app.CmdUninstall(st, cfg, args)
	case "path":
		runErr = app.CmdPath(st, cfg, args)
	case "open":
		runErr = app.CmdOpen(st, cfg, args)
	case "help", "-h", "--help", "-help":
		fmt.Print(app.Usage)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n%s", cmd, app.Usage)
		os.Exit(2)
	}
	if runErr != nil {
		fatal(runErr)
	}
}

func fatal(v any) {
	fmt.Fprintln(os.Stderr, "错误:", v)
	os.Exit(1)
}

func splitArgs(args []string) (file, config string, noBackup bool, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-file" || a == "--file" || a == "-f":
			if i+1 < len(args) {
				file = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-file="):
			file = strings.TrimPrefix(a, "-file=")
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case strings.HasPrefix(a, "-f="):
			file = strings.TrimPrefix(a, "-f=")
		case a == "-config" || a == "--config" || a == "-c":
			if i+1 < len(args) {
				config = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config="):
			config = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			config = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			config = strings.TrimPrefix(a, "-c=")
		case a == "-no-backup" || a == "--no-backup":
			noBackup = true
		default:
			rest = append(rest, a)
		}
	}
	return
}