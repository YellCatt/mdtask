package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 事件类型
const (
	evNew     = "new"
	evDone    = "done"
	evCancel  = "cancel"
	evRemoved = "removed"
)

type daemonEvent struct {
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	ID    string    `json:"id"`
	Title string    `json:"title"`
}

type daemonState struct {
	LastReport time.Time         `json:"last_report"`
	LastFire   string            `json:"last_fire"`
	Snapshot   map[string]string `json:"snapshot"` // id -> 状态
	Events     []daemonEvent     `json:"events"`
}

type clock struct{ h, m int }

type daemon struct {
	times    []clock
	interval time.Duration
	openFile bool
	state    *daemonState
	stateDir string
}

func cmdDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	at := fs.String("at", strings.Join(cfg.Report.Times, ","), "日报时间，逗号分隔，如 21:00,05:00")
	interval := fs.Int("interval", cfg.Report.Interval, "扫描 md 变化的间隔（秒）")
	open := fs.Bool("open", cfg.Report.Open, "生成日报后用默认程序打开")
	once := fs.Bool("once", false, "立刻生成一份日报并退出（用于测试）")
	fs.Parse(args)

	d := &daemon{interval: time.Duration(*interval) * time.Second, openFile: *open}
	for _, s := range strings.Split(*at, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		c, err := parseClock(s)
		if err != nil {
			fatal(err)
		}
		d.times = append(d.times, c)
	}
	if len(d.times) == 0 {
		fatal("至少要给一个日报时间")
	}
	sort.Slice(d.times, func(i, j int) bool {
		return d.times[i].h*60+d.times[i].m < d.times[j].h*60+d.times[j].m
	})

	d.loadState()

	if *once {
		d.poll()
		d.report(time.Now(), cfg.Report.Notify)
		return
	}

	logf("MDTask 常驻已启动，数据文件: %s", st.path)
	logf("扫描间隔 %s，日报时间 %s", d.interval, strings.Join(strings.Split(*at, ","), " / "))
	logf("日报目录: %s", d.dailyDir())
	logf("按 Ctrl+C 退出")

	d.poll() // 先建一次快照

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for range ticker.C {
		d.poll()
		if fire, ok := d.due(time.Now()); ok {
			d.report(fire, cfg.Report.Notify)
		}
	}
}

func parseClock(s string) (clock, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return clock{}, fmt.Errorf("时间格式应为 HH:MM: %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return clock{}, fmt.Errorf("时间格式应为 HH:MM: %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return clock{}, fmt.Errorf("时间格式应为 HH:MM: %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return clock{}, fmt.Errorf("时间超出范围: %q", s)
	}
	return clock{h, m}, nil
}

func logf(format string, a ...any) {
	fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// ---------- 状态持久化 ----------

func (d *daemon) statePath() string {
	return filepath.Join(filepath.Dir(st.path), ".mdtask-daemon.json")
}

func (d *daemon) dailyDir() string {
	dir := cfg.Report.Dir
	if dir == "" {
		dir = ".mdtask-daily"
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(filepath.Dir(st.path), dir)
}

func (d *daemon) loadState() {
	d.state = &daemonState{Snapshot: map[string]string{}}
	b, err := os.ReadFile(d.statePath())
	if err != nil {
		return
	}
	var s daemonState
	if json.Unmarshal(b, &s) != nil {
		return
	}
	if s.Snapshot == nil {
		s.Snapshot = map[string]string{}
	}
	d.state = &s
}

func (d *daemon) saveState() {
	b, _ := json.MarshalIndent(d.state, "", "  ")
	os.WriteFile(d.statePath(), b, 0o644)
}

// ---------- 扫描变化 ----------

func (d *daemon) poll() {
	tasks, _, err := st.List()
	if err != nil {
		return
	}
	arch, _ := st.ListArchive()

	cur := map[string]string{}
	title := map[string]string{}
	for _, t := range tasks {
		cur[t.ID] = t.Status
		title[t.ID] = t.Title
	}
	for _, t := range arch {
		cur[t.ID] = t.Status
		title[t.ID] = t.Title
	}

	first := len(d.state.Snapshot) == 0
	now := time.Now()

	if !first {
		for id, s := range cur {
			old, existed := d.state.Snapshot[id]
			if !existed {
				d.state.Events = append(d.state.Events, daemonEvent{now, evNew, id, title[id]})
				if k := closedKind(s); k != "" { // 新建就是结束态
					d.state.Events = append(d.state.Events, daemonEvent{now, k, id, title[id]})
				}
				continue
			}
			if old != s {
				if k := closedKind(s); k != "" {
					d.state.Events = append(d.state.Events, daemonEvent{now, k, id, title[id]})
				}
			}
		}
		for id := range d.state.Snapshot {
			if _, ok := cur[id]; !ok {
				d.state.Events = append(d.state.Events, daemonEvent{now, evRemoved, id, ""})
			}
		}
	}

	d.state.Snapshot = cur
	// 只保留最近 30 天的事件
	cutoff := now.AddDate(0, 0, -30)
	kept := d.state.Events[:0]
	for _, e := range d.state.Events {
		if e.At.After(cutoff) {
			kept = append(kept, e)
		}
	}
	d.state.Events = kept
	d.saveState()
}

func closedKind(s string) string {
	d := statusDefOf(s)
	if d == nil || !d.Closed {
		return ""
	}
	if d.Key == stCancel {
		return evCancel
	}
	return evDone
}

// ---------- 触发判断 ----------

func (d *daemon) due(now time.Time) (time.Time, bool) {
	for _, c := range d.times {
		fire := time.Date(now.Year(), now.Month(), now.Day(), c.h, c.m, 0, 0, now.Location())
		if now.Before(fire) {
			continue
		}
		key := fire.Format("2006-01-02T15:04")
		if d.state.LastFire == key {
			continue
		}
		if now.Sub(fire) > 2*time.Hour { // 错过了太久就不补报
			continue
		}
		return fire, true
	}
	return time.Time{}, false
}

// ---------- 生成日报 ----------

func (d *daemon) report(fire time.Time, notifyIt bool) {
	from := d.state.LastReport
	if from.IsZero() {
		from = fire.Add(-12 * time.Hour)
	}
	var done, cancel, fresh, removed []daemonEvent
	for _, e := range d.state.Events {
		if e.At.Before(from) || e.At.After(fire) {
			continue
		}
		switch e.Kind {
		case evDone:
			done = append(done, e)
		case evCancel:
			cancel = append(cancel, e)
		case evNew:
			fresh = append(fresh, e)
		case evRemoved:
			removed = append(removed, e)
		}
	}

	tasks, _, err := st.List()
	if err != nil {
		tasks = nil
	}
	var doing, hold, todo, overdue []Task
	today := time.Now().Truncate(24 * time.Hour)
	for _, t := range tasks {
		switch {
		case statusDefOf(t.Status) != nil && statusDefOf(t.Status).Key == stDoing:
			doing = append(doing, t)
		case statusDefOf(t.Status) != nil && statusDefOf(t.Status).Key == stHold:
			hold = append(hold, t)
		case strings.TrimSpace(t.Status) == "":
			todo = append(todo, t)
		}
		if !isClosedStatus(t.Status) {
			if due, err := time.Parse("2006-01-02", strings.TrimSpace(t.Due)); err == nil && due.Before(today) {
				overdue = append(overdue, t)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s 日报\n\n", fire.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "> 统计区间 %s → %s\n\n", from.Format("01-02 15:04"), fire.Format("01-02 15:04"))

	fmt.Fprintf(&b, "## ✅ 新完成（%d）\n\n", len(done))
	writeEvents(&b, done, "这段时间没有完成的任务")
	fmt.Fprintf(&b, "\n## 🆕 新增（%d）\n\n", len(fresh))
	writeEvents(&b, fresh, "这段时间没有新增任务")
	if len(cancel) > 0 || len(removed) > 0 {
		fmt.Fprintf(&b, "\n## 🔴 新取消 / 删除（%d）\n\n", len(cancel)+len(removed))
		writeEvents(&b, cancel, "")
		writeEvents(&b, removed, "")
	}

	fmt.Fprintf(&b, "\n## 当前状态\n\n")
	fmt.Fprintf(&b, "- ⏸️ 进行中 %d 项\n", len(doing))
	for _, t := range doing {
		fmt.Fprintf(&b, "  - #%s %s%s\n", t.ID, t.Title, dueSuffix(t))
	}
	fmt.Fprintf(&b, "- ❌ 停滞 %d 项\n", len(hold))
	for _, t := range hold {
		fmt.Fprintf(&b, "  - #%s %s%s\n", t.ID, t.Title, dueSuffix(t))
	}
	fmt.Fprintf(&b, "- ⬜ 待办 %d 项\n", len(todo))
	for _, t := range todo {
		fmt.Fprintf(&b, "  - #%s %s%s\n", t.ID, t.Title, dueSuffix(t))
	}
	if len(overdue) > 0 {
		fmt.Fprintf(&b, "\n## ⚠️ 已逾期（%d）\n\n", len(overdue))
		for _, t := range overdue {
			fmt.Fprintf(&b, "- #%s %s（截止 %s）\n", t.ID, t.Title, strings.TrimSpace(t.Due))
		}
	}

	dir := d.dailyDir()
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, fire.Format("2006-01-02")+".md")

	old := ""
	if b0, err := os.ReadFile(path); err == nil {
		old = string(b0)
	}
	content := b.String()
	if old != "" {
		content = old + "\n---\n\n" + content
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logf("写日报失败: %v", err)
		return
	}

	summary := fmt.Sprintf("✅完成 %d · 🆕新增 %d · ⏸️进行中 %d · ❌停滞 %d · ⚠️逾期 %d",
		len(done), len(fresh), len(doing), len(hold), len(overdue))

	d.state.LastReport = fire
	d.state.LastFire = fire.Format("2006-01-02T15:04")
	d.saveState()

	logf("日报已生成: %s", path)
	logf(summary)
	if notifyIt {
		notify("MDTask "+fire.Format("15:04")+" 日报", summary+"\n"+path)
	}
	if d.openFile {
		openFile(path)
	}
}

func writeEvents(b *strings.Builder, es []daemonEvent, empty string) {
	if len(es) == 0 {
		if empty != "" {
			fmt.Fprintf(b, "%s\n", empty)
		}
		return
	}
	for _, e := range es {
		fmt.Fprintf(b, "- #%s %s  `%s`\n", e.ID, e.Title, e.At.Format("01-02 15:04"))
	}
}

func dueSuffix(t Task) string {
	d := strings.TrimSpace(t.Due)
	if d == "" {
		return ""
	}
	return "（截止 " + d + "）"
}

// ---------- 系统通知 ----------

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

// ---------- 开机自启 ----------

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
