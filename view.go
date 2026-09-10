package main

import (
	"fmt"
	"sort"
	"strings"
)

// printTable 把任务渲染成终端表格，列宽按显示宽度（emoji/中文算 2 列）对齐
func printTable(tasks []Task, summary bool) {
	if len(tasks) == 0 {
		fmt.Println(paint(cDim, "（没有任务）"))
		return
	}

	headers := []string{"状态", "ID", "优先级", "标题", "截止日期", "备注"}
	rows := make([][]string, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, []string{
			statusLabel(t.Status),
			t.ID,
			prioLabel(t.Priority),
			truncate(oneLine(t.Title), 44),
			t.Due,
			truncate(oneLine(t.Note), 30),
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
		cells[0] = pad(paint(statusColor(t.Status), statusLabel(t.Status)), widths[0])
		cells[1] = paint(cDim, cells[1])
		cells[2] = pad(paint(prioColor(t.Priority), prioLabel(t.Priority)), widths[2])
		cells[4] = pad(paint(dueColor(t, t.Due), t.Due), widths[4])
		cells[5] = paint(cGray, cells[5])
		if isClosedStatus(t.Status) { // 结束态整行淡显
			cells[3] = paint(cDim, cells[3])
		}
		fmt.Println(strings.Join(cells, "  "))
	}

	if !summary {
		return
	}
	n := map[string]int{}
	for _, t := range tasks {
		if d := statusDefOf(t.Status); d != nil {
			n[d.Key]++
		} else {
			n["待办"]++
		}
	}
	parts := make([]string, 0, len(statusDefs)+1)
	if n["待办"] > 0 {
		parts = append(parts, fmt.Sprintf("待办 %d", n["待办"]))
	}
	for i := range statusDefs {
		if n[statusDefs[i].Key] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", statusDefs[i].Label, n[statusDefs[i].Key]))
		}
	}
	fmt.Println()
	fmt.Println(paint(cGray, fmt.Sprintf("共 %d 项 · %s", len(tasks), strings.Join(parts, " · "))))
}

// ---------- 辅助 ----------

func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
}

func allText(t Task) string {
	var b strings.Builder
	b.WriteString(t.ID + " " + t.Title + " " + t.Status + " " + t.Priority + " " + t.Due + " " + t.Note)
	for _, kv := range sortedExtra(t.Extra) {
		b.WriteString(" " + kv[1])
	}
	return b.String()
}

func sortedExtra(m map[string]string) [][2]string {
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

func filterTasks(tasks []Task, f func(Task) bool) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if f(t) {
			out = append(out, t)
		}
	}
	return out
}

// sortTasks 待办 → 进行中 → 停滞 → 完成/取消，同级按优先级、截止日期
func sortTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		a, b := statusRankOf(tasks[i].Status), statusRankOf(tasks[j].Status)
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

func prioRank(t Task) int {
	switch strings.ToLower(strings.TrimSpace(t.Priority)) {
	case "high":
		return 3
	case "mid":
		return 2
	case "low":
		return 1
	}
	return 0
}

// dueKey 没写截止日期的排最后
func dueKey(v string) string {
	if strings.TrimSpace(v) == "" {
		return "9999-99-99"
	}
	return v
}

func findTask(tasks []Task, id string) (Task, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
