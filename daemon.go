package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	LastReport  time.Time         `json:"last_report"`
	LastFire    string            `json:"last_fire"`
	LastWeekly  string            `json:"last_weekly"`  // 已出过的周报周期，如 2026-W37
	LastMonthly string            `json:"last_monthly"` // 已出过的月报周期，如 2026-09
	LastYearly  string            `json:"last_yearly"`  // 已出过的年报周期，如 2026
	Snapshot    map[string]string `json:"snapshot"`     // id -> 状态
	Events      []daemonEvent     `json:"events"`
}

// eventKeepDays 事件保留天数，要撑得住年报（按月）跨度
const eventKeepDays = 400

type clock struct{ h, m int }

type daemon struct {
	times    []clock
	interval time.Duration
	openFile bool
	state    *daemonState
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
		d.reportPeriod(kDaily, time.Now(), false, cfg.Report.Notify, d.openFile)
		return
	}

	logf("MDTask 常驻已启动，数据文件: %s", st.path)
	logf("扫描间隔 %s，日报时间 %s", d.interval, strings.Join(strings.Split(*at, ","), " / "))
	logf("报告目录: %s（周报 %s / 月报 %s / 年报 %s）",
		d.dailyDir(), weeklyDesc(), monthlyDesc(), yearlyDesc())
	logf("按 Ctrl+C 退出")

	d.poll() // 先建一次快照

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for range ticker.C {
		d.poll()
		if fire, ok := d.due(time.Now()); ok {
			d.reportPeriod(kDaily, fire, false, cfg.Report.Notify, d.openFile)
			d.reportScheduled(fire)
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

// poll 对比上次快照，把新增 / 完成 / 取消 / 删除记成事件
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
	// 只保留最近 eventKeepDays 天的事件
	cutoff := now.AddDate(0, 0, -eventKeepDays)
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

// due 返回该出日报的时间点；错过超过 2 小时就不补报
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
		if now.Sub(fire) > 2*time.Hour {
			continue
		}
		return fire, true
	}
	return time.Time{}, false
}
