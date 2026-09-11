package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/logger"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func main() {
	time.Local = time.FixedZone("CST", 8*3600)

	file, configPath, noBackup, _ := splitArgs(os.Args[1:])

	cfg, configPath, err := config.Load(configPath)
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

	root, err := os.Getwd()
	if err != nil || root == "" {
		if exe, e2 := os.Executable(); e2 == nil {
			root = filepath.Dir(exe)
		} else {
			root = "."
		}
	}
	if rootAbs, e3 := filepath.Abs(root); e3 == nil {
		root = rootAbs
	}

	if err := logger.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 日志初始化失败: %v\n", err)
	}

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup, cfg.Archive.Heading)

	runDaemon(st, cfg, root, configPath)
}

func runDaemon(st *store.Store, cfg *config.Config, root, configPath string) {
	logger.Info("MDTask 启动",
		"dir", st.Dir,
		"root", root,
		"config_path", configPath,
		"backup", st.Backup,
	)
	logger.Debug("当前配置摘要",
		"report_times", cfg.Report.Times,
		"report_interval", cfg.Report.Interval,
		"weekly", cfg.Report.Weekly,
		"monthly", cfg.Report.Monthly,
		"yearly", cfg.Report.Yearly,
		"mail_host", cfg.Mail.SMTPHost,
		"mail_port", cfg.Mail.SMTPPort,
		"mail_from", cfg.Mail.FromEmail,
	)

	known, err := st.CollectKnownIDs()
	if err != nil {
		logger.Error("初始化 CollectKnownIDs 失败", "err", err)
		fatal(fmt.Errorf("初始化失败: %w", err))
	}
	logger.Info("初始化完成", "known_tasks", len(known))

	dumpAllReports(st, cfg, root)

	logger.Info("启动邮件调度 goroutine")
	go runMailer(st, cfg)

	interval := cfg.Report.Interval
	if interval <= 0 {
		interval = 10
	}
	logger.Info("进入主循环: 定时扫描任务目录", "interval_seconds", interval)
	for {
		time.Sleep(time.Duration(interval) * time.Second)
		known, err = st.TouchAddedDates(known)
		if err != nil {
			logger.Error("TouchAddedDates 扫描出错", "err", err)
		}
	}
}

func fatal(v any) {
	logger.Error("fatal 退出", "err", v)
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
