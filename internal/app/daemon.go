package app

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

	"mdtask/internal/config"
	"mdtask/internal/notify"
	"mdtask/internal/status"
	"mdtask/internal/store"
)

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
	LastWeekly  string            `json:"last_weekly"`
	LastMonthly string            `json:"last_monthly"`
	LastYearly  string            `json:"last_yearly"`
	Snapshot    map[string]string `json:"snapshot"`
	Events      []daemonEvent     `json:"events"`
}

const eventKeepDays = 400

type clock struct{ h, m int }

type daemon struct {
	st       *store.Store
	cfg      *config.Config
	times    []clock
	interval time.Duration
	openFile bool
	state    *daemonState
}

func CmdDaemon(st *store.Store, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	at := fs.String("at", strings.Join(cfg.Report.Times, ","), "日报时间，逗号分隔，如 21:00,05:00")
	interval := fs.Int("interval", cfg.Report.Interval, "扫描 md 变化的间隔（秒）")
	open := fs.Bool("open", cfg.Report.Open, "生成日报后用默认程序打开")
	once := fs.Bool("once", false, "立刻生成一份日报并退出（用于测试）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	d := &daemon{st: st, cfg: cfg, interval: time.Duration(*interval) * time.Second, openFile: *open}
	for _, s := range strings.Split(*at, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		c, err := parseClock(s)
		if err != nil {
			return err
		}
		d.times = append(d.times, c)
	}
	if len(d.times) == 0 {
		return fmt.Errorf("至少要给一个日报时间")
	}
	sort.Slice(d.times, func(i, j int) bool {
		return d.times[i].h*60+d.times[i].m < d.times[j].h*60+d.times[j].m
	})

	d.loadState()

	if *once {
		d.poll()
		CmdReport(st, cfg, []string{"-type", "daily", "-open", boolToStr(d.openFile)})
		return nil
	}

	logf("MDTask 常驻已启动，数据目录: %s", st.Dir)
	logf("扫描间隔 %s，日报时间: %s", d.interval, strings.Join(strings.Split(*at, ","), " / "))
	logf("报告目录: %s", d.dailyDir())
	logf("按 Ctrl+C 退出")

	d.poll()
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for range ticker.C {
		d.poll()
		if fire, ok := d.due(time.Now()); ok {
			CmdReport(st, cfg, []string{"-type", "daily", "-date", fire.Format("2006-01-02")})
			d.reportScheduled(fire)
		}
	}
	return nil
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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

func (d *daemon) statePath() string {
	return filepath.Join(d.st.Dir, ".mdtask-daemon.json")
}

func (d *daemon) dailyDir() string {
	dir := d.cfg.Report.Dir
	if dir == "" {
		dir = ".mdtask-daily"
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(d.st.Dir, dir)
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

func (d *daemon) poll() {
	tasks, _, err := d.st.List()
	if err != nil {
		return
	}
	arch, _ := d.st.ListArchive()

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
				if k := closedKind(s); k != "" {
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
	d := status.DefOf(s)
	if d == nil || !d.Closed {
		return ""
	}
	if d.Key == status.Cancel {
		return evCancel
	}
	return evDone
}

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

func (d *daemon) reportScheduled(fire time.Time) {
	d.state.LastFire = fire.Format("2006-01-02T15:04")
	d.saveState()
}

func notifyFallback(title, text string) {
	notify.Notify(title, text)
}