package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/logger"
	"mdtask/internal/status"
	"mdtask/internal/store"
	"mdtask/internal/ui"
	"mdtask/internal/util"
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

	if err := logger.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 日志初始化失败: %v\n", err)
	}

	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup, cfg.Archive.Heading)

	runDaemon(st, cfg, configPath)
}

func runDaemon(st *store.Store, cfg *config.Config, configPath string) {
	logger.Info("MDTask 启动",
		"dir", st.Dir,
		"config_path", configPath,
		"backup", st.Backup,
	)
	logger.Debug("当前配置摘要",
		"report_times", cfg.Report.Times,
		"report_interval", cfg.Report.Interval,
		"weekly", cfg.Report.Weekly,
		"monthly", cfg.Report.Monthly,
		"yearly", cfg.Report.Yearly,
		"archive_auto", cfg.Archive.Auto,
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

	autoArchive(st, cfg)

	dumpAllReports(st)

	logger.Info("启动邮件调度 goroutine")
	go runMailer(st, cfg)

	interval := cfg.Report.Interval
	if interval <= 0 {
		interval = 10
	}

	// 记录任务目录指纹；之后只要指纹变化（md 被改动）就触发自动归档。
	lastSig := dirSignature(st.Dir)
	logger.Info("进入主循环: 定时扫描任务目录", "interval_seconds", interval)
	for {
		time.Sleep(time.Duration(interval) * time.Second)

		known, err = st.TouchAddedDates(known)
		if err != nil {
			logger.Error("TouchAddedDates 扫描出错", "err", err)
		}

		sig := dirSignature(st.Dir)
		if sig == lastSig {
			continue
		}
		lastSig = sig
		logger.Info("检测到任务文件变化，触发自动归档")
		autoArchive(st, cfg)
		// 归档可能改写文件，刷新指纹，避免下一轮重复触发。
		if s2 := dirSignature(st.Dir); s2 != "" {
			lastSig = s2
		}
	}
}

// autoArchive 按 archive.auto 配置自动归档：
// -1 关闭；0 只要有任务结束就归档；N 表示截止日期早于 N 天前才归档。
// 是否连「停滞」一起搬走由 archive.include_stuck 决定。
func autoArchive(st *store.Store, cfg *config.Config) {
	days := cfg.Archive.Auto
	if v := strings.TrimSpace(os.Getenv("MDTASK_AUTO_ARCHIVE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			days = n
		} else {
			days = 0
		}
	}
	if days < 0 {
		return
	}

	cutoff := time.Now()
	if days > 0 {
		cutoff = cutoff.AddDate(0, 0, -days)
	}
	includeStuck := cfg.Archive.IncludeStuck

	pred := func(t store.Task) bool {
		d := status.DefOf(t.Status)
		if d == nil {
			return false
		}
		if !d.Closed && !(includeStuck && status.IsHold(t.Status)) {
			return false
		}
		if days == 0 {
			return true
		}
		due, err := util.ParseDate(t.Due)
		if err != nil {
			return false
		}
		return due.Before(cutoff)
	}

	var moved int
	if err := st.Update(func(s *store.Store) error {
		moved = s.Archive(pred)
		return nil
	}); err != nil {
		logger.Error("自动归档失败", "err", err)
		return
	}
	if moved > 0 {
		logger.Info("自动归档完成", "moved", moved)
	}
}

// dirSignature 计算任务目录下所有 md 文件的指纹（名称+大小+修改时间），
// 用来判断 ./tasks 下的文件是否被改动过。
func dirSignature(dir string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		info, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s|%d|%d\n", n, info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
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