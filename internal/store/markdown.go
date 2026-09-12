package store

import (
	"strings"

	"mdtask/internal/logger"
)

// ---------- 扫描 md ----------

func defaultDoc() string {
	return "# 任务清单\n\n" +
		"| 状态 | ID | 标题 | 优先级 | 截止日期 | 备注 | 添加日期 |\n" +
		"|------|----|------|--------|----------|------|----------|\n"
}

func headerRows(cols []string) []string {
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	return []string{"| " + strings.Join(cols, " | ") + " |", "| " + strings.Join(sep, " | ") + " |"}
}

func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

func isSeparatorRow(line string) bool {
	l := strings.TrimSpace(line)
	if !strings.HasPrefix(l, "|") {
		return false
	}
	body := strings.NewReplacer("|", "", " ", "", ":", "", "\t", "").Replace(l)
	if body == "" {
		return false
	}
	for _, r := range body {
		if r != '-' {
			return false
		}
	}
	return true
}

// locateTableFrom 从 from 行开始找第一张 markdown 表格，返回 [表头起, 结束行)
func locateTableFrom(lines []string, from int) (int, int) {
	for i := from; i+1 < len(lines); i++ {
		if !isTableRow(lines[i]) || isSeparatorRow(lines[i]) {
			continue
		}
		if !isSeparatorRow(lines[i+1]) {
			continue
		}
		end := i + 2
		for end < len(lines) && isTableRow(lines[end]) {
			end++
		}
		logger.Debug("locateTableFrom 找到表格", "from", from, "start", i, "end", end)
		return i, end
	}
	logger.Debug("locateTableFrom 未找到表格", "from", from)
	return -1, -1
}

// findHeadingFrom 找指定标题的行号（"## 归档"）
func findHeadingFrom(lines []string, title string, from int) int {
	want := strings.ToLower(strings.TrimSpace(title))
	for i := from; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "#") {
			continue
		}
		h := strings.ToLower(strings.TrimSpace(strings.TrimLeft(l, "#")))
		if h == want {
			logger.Debug("findHeadingFrom 找到标题", "title", title, "pos", i)
			return i
		}
	}
	logger.Debug("findHeadingFrom 未找到标题", "title", title, "from", from)
	return -1
}

// ---------- 单元格 ----------

// splitRow 按未转义的 | 拆分单元格，并还原 \| 和 <br>
func splitRow(line string) []string {
	l := strings.TrimSpace(line)
	l = strings.TrimPrefix(l, "|")
	l = strings.TrimSuffix(l, "|")

	var cells []string
	var cur strings.Builder
	for i := 0; i < len(l); {
		if l[i] == '\\' && i+1 < len(l) && l[i+1] == '|' {
			cur.WriteByte('|')
			i += 2
			continue
		}
		if l[i] == '|' {
			cells = append(cells, cur.String())
			cur.Reset()
			i++
			continue
		}
		cur.WriteByte(l[i])
		i++
	}
	cells = append(cells, cur.String())
	for i := range cells {
		cells[i] = unescapeCell(strings.TrimSpace(cells[i]))
	}
	return cells
}

func unescapeCell(s string) string {
	s = strings.ReplaceAll(s, "\\|", "|")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	return s
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return s
}

func escapeAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = escapeCell(s)
	}
	return out
}