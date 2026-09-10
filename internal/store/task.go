package store

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"mdtask/internal/status"
)

var ErrNoDate = errors.New("no date")

// Task 一行任务。不认识的列原样存在 Extra 里，写回时不会丢失。
type Task struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Priority string            `json:"priority"`
	Due      string            `json:"due"`
	Note     string            `json:"note"`
	Added    string            `json:"added"`
	DoneAt   string            `json:"done_at"`
	Extra    map[string]string `json:"extra,omitempty"`
	Source   string            `json:"-"`
}

func (t *Task) Get(col string) string {
	switch canonField(col) {
	case FID:
		return t.ID
	case FTitle:
		return t.Title
	case FStatus:
		return t.Status
	case FPriority:
		return t.Priority
	case FDue:
		return t.Due
	case FNote:
		return t.Note
	case FAdded:
		return t.Added
	case FDoneAt:
		return t.DoneAt
	}
	return t.Extra[col]
}

func (t *Task) Set(col, v string) {
	switch canonField(col) {
	case FID:
		t.ID = v
		return
	case FTitle:
		t.Title = v
		return
	case FStatus:
		t.Status = v
		return
	case FPriority:
		t.Priority = v
		return
	case FDue:
		t.Due = v
		return
	case FNote:
		t.Note = v
		return
	case FAdded:
		t.Added = v
		return
	case FDoneAt:
		t.DoneAt = v
		return
	}
	if t.Extra == nil {
		t.Extra = map[string]string{}
	}
	t.Extra[col] = v
}

func (t *Task) isBlank() bool {
	if strings.TrimSpace(t.ID+t.Title+t.Status+t.Priority+t.Due+t.Note+t.Added+t.DoneAt) != "" {
		return false
	}
	for _, v := range t.Extra {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// ---------- 表格 ----------

// tbl md 文件里的一张表，记录它占的行区间
type tbl struct {
	start, end int
	Columns    []string
	fields     []string
	tasks      []Task
}

// parse 解析表头和数据行；补齐缺失的标准列、给空 ID 编号，返回文件是否需要重写
func (tb *tbl) parse(lines []string) (dirty bool) {
	seen := map[string]bool{}
	for _, c := range splitRow(lines[tb.start]) {
		c = strings.TrimSpace(c)
		if c == "" {
			c = "列" + strconv.Itoa(len(tb.Columns)+1)
		}
		if seen[c] {
			c = c + strconv.Itoa(len(tb.Columns)+1)
		}
		seen[c] = true
		tb.Columns = append(tb.Columns, c)
		tb.fields = append(tb.fields, canonField(c))
	}
	for _, f := range stdColumns { // 缺的标准列补上
		has := false
		for _, g := range tb.fields {
			if g == f {
				has = true
				break
			}
		}
		if !has {
			tb.Columns = append(tb.Columns, f)
			tb.fields = append(tb.fields, f)
			dirty = true
		}
	}

	tb.tasks = nil
	for i := tb.start + 2; i < tb.end; i++ {
		cells := splitRow(lines[i])
		var t Task
		for j, col := range tb.Columns {
			v := ""
			if j < len(cells) {
				v = cells[j]
			}
			t.Set(col, v)
		}
		if t.isBlank() {
			continue
		}
		tb.tasks = append(tb.tasks, t)
	}

	// 补编号
	used := map[string]bool{}
	for _, t := range tb.tasks {
		if s := strings.TrimSpace(t.ID); s != "" {
			used[s] = true
		}
	}
	for i := range tb.tasks {
		if strings.TrimSpace(tb.tasks[i].ID) == "" {
			tb.tasks[i].ID = nextFreeID(used)
			used[tb.tasks[i].ID] = true
			dirty = true
		}
	}
	return dirty
}

func (tb *tbl) render() []string {
	rows := make([]string, 0, len(tb.tasks)+2)
	rows = append(rows, "| "+strings.Join(escapeAll(tb.Columns), " | ")+" |")
	sep := make([]string, len(tb.Columns))
	for i := range sep {
		sep[i] = "---"
	}
	rows = append(rows, "| "+strings.Join(sep, " | ") + " |")
	for i := range tb.tasks {
		vals := make([]string, len(tb.Columns))
		for j, col := range tb.Columns {
			vals[j] = escapeCell(tb.tasks[i].Get(col))
		}
		rows = append(rows, "| "+strings.Join(vals, " | ")+" |")
	}
	return rows
}

func nextFreeID(used map[string]bool) string {
	for i := 1; ; i++ {
		id := strconv.Itoa(i)
		if !used[id] {
			return id
		}
	}
}

func cloneTasks(ts []Task) []Task {
	out := make([]Task, 0, len(ts))
	for _, t := range ts {
		c := t
		if t.Extra != nil {
			c.Extra = make(map[string]string, len(t.Extra))
			for k, v := range t.Extra {
				c.Extra[k] = v
			}
		}
		out = append(out, c)
	}
	return out
}

func (t *Task) Closed() bool {
	return status.IsClosed(t.Status)
}

func Today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, ErrNoDate
	}
	layouts := []string{"2006-01-02", "2006/01/02", "2006.01.02", "01-02", "01/02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			if l == "01-02" || l == "01/02" {
				t = time.Date(time.Now().Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			}
			return t, nil
		}
	}
	return time.Time{}, ErrNoDate
}