package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mdtask/internal/config"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func main() {
	file, configPath, noBackup, _ := splitArgs(os.Args[1:])

	cfg, _, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	if file == "" {
		file = os.Getenv("MDTASK_FILE")
	}
	if file == "" {
		file = cfg.Dir
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		fatal(err)
	}

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup)

	if err := st.Load(); err != nil {
		fatal(fmt.Errorf("打开 %s 失败: %w", abs, err))
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