package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// report 汇总上次日报以来的变化，写成 md 存到日报目录，再弹一条通知
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
		def := statusDefOf(t.Status)
		switch {
		case def != nil && def.Key == stDoing:
			doing = append(doing, t)
		case def != nil && def.Key == stHold:
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
