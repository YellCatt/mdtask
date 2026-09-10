package ui

import (
	"fmt"
	"sort"
	"strings"

	"mdtask/internal/status"
	"mdtask/internal/store"
)

func PrintTable(tasks []store.Task, summary bool) {
	if len(tasks) == 0 {
		fmt.Println(paint(cDim, "（没有任务）"))
		return
	}

	headers := []string{"状态", "ID", "优先级", "标题", "截止日期", "备注"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			status.Label(t.Status),
			t.ID,
			prioLabel(t.Priority),
			Truncate(oneLine(t.Title), 44),
			t.Due,
			Truncate(oneLine(t.Note), 30),
		})
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = dispWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if w := dispWidth(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	head := make([]string, len(headers))
	for i, h := range headers {
		head[i] = paint(cBold, pad(h, widths[i]))
	}
	fmt.Println(strings.Join(head, "  "))

	for i, r := range rows {
		t := tasks[i]
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = pad(c, widths[j])
		}
		cells[0] = pad(paint(statusColor(t.Status), status.Label(t.Status)), widths[0])
		cells[1] = paint(cDim, cells[1])
		cells[2] = pad(paint(PriorityColor(t.Priority), prioLabel(t.Priority)), widths[2])
		cells[4] = pad(paint(DueColor(t), DueText(t)), widths[4])
		cells[5] = paint(cGray, cells[5])
		if t.Closed() {
			cells[3] = paint(cDim, cells[3])
		}
		fmt.Println(strings.Join(cells, "  "))
	}

	if !summary {
		return
	}
	n := map[string]int{}
	for _, t := range tasks {
		if d := status.DefOf(t.Status); d != nil {
			n[d.Key]++
		} else {
			n["待办"]++
		}
	}
	parts := make([]string, 0, 5)
	if n["待办"] > 0 {
		parts = append(parts, fmt.Sprintf("待办 %d", n["待办"]))
	}
	for _, d := range []*status.StatusDef{
		status.DefOf(status.Doing),
		status.DefOf(status.Hold),
		status.DefOf(status.Done),
		status.DefOf(status.Cancel),
	} {
		if d != nil && n[d.Key] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", d.Label, n[d.Key]))
		}
	}
	fmt.Println()
	fmt.Println(paint(cGray, fmt.Sprintf("· %d 项 · %s", len(tasks), strings.Join(parts, " · "))))
}

// ---------- 辅助 ----------

func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func AllText(t store.Task) string {
	var b strings.Builder
	b.WriteString(t.ID + " " + t.Title + " " + t.Status + " " + t.Priority + " " + t.Due + " " + t.Note)
	for _, kv := range SortedExtra(t.Extra) {
		b.WriteString(" " + kv[1])
	}
	return b.String()
}

func SortedExtra(m map[string]string) [][2]string {
	if len(m) == 0 {
		return nil
	}
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func FilterTasks(tasks []store.Task, f func(store.Task) bool) []store.Task {
	out := make([]store.Task, 0, len(tasks))
	for _, t := range tasks {
		if f(t) {
			out = append(out, t)
		}
	}
	return out
}

func SortTasks(tasks []store.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := status.RankOf(tasks[i].Status), status.RankOf(tasks[j].Status)
		if a != b {
			return a < b
		}
		a, b = prioRank(tasks[i]), prioRank(tasks[j])
		if a != b {
			return a > b
		}
		return dueKey(tasks[i].Due) < dueKey(tasks[j].Due)
	})
}

func prioLabel(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "p0", "p1", "p2", "p3", "p4":
		return strings.ToUpper(strings.TrimSpace(p))
	default:
		return ""
	}
}

func prioRank(t store.Task) int {
	switch strings.ToLower(strings.TrimSpace(t.Priority)) {
	case "p0":
		return 5
	case "p1":
		return 4
	case "p2":
		return 3
	case "p3":
		return 2
	case "p4":
		return 1
	}
	return 0
}

func dueKey(v string) string {
	if strings.TrimSpace(v) == "" {
		return "9999-99-99"
	}
	return v
}

func FindTask(tasks []store.Task, id string) (store.Task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return store.Task{}, false
}

func statusColor(s string) string {
	if status.IsClosed(s) {
		return cGray
	}
	switch strings.TrimSpace(s) {
	case status.Doing:
		return cBlue
	case status.Hold:
		return cYel
	default:
		return ""
	}
}