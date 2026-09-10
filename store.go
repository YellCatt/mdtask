package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 标准字段列名，跟 md 表头对应（通过别名表宽容匹配）
const (
	FID       = "ID"
	FTitle    = "标题"
	FStatus   = "状态"
	FPriority = "优先级"
	FDue      = "截止日期"
	FNote     = "备注"
)

var stdColumns = []string{FStatus, FID, FTitle, FPriority, FDue, FNote}

// 列名别名 -> 标准字段（key 已归一化）
var fieldAliases = map[string]string{
	"id": FID, "编号": FID, "序号": FID, "no": FID,
	"标题": FTitle, "title": FTitle, "任务": FTitle, "名称": FTitle, "name": FTitle, "内容": FTitle,
	"状态": FStatus, "status": FStatus, "state": FStatus, "进度": FStatus,
	"优先级": FPriority, "priority": FPriority, "pri": FPriority, "级别": FPriority, "重要度": FPriority,
	"截止日期": FDue, "截止": FDue, "due": FDue, "duedate": FDue, "deadline": FDue, "日期": FDue, "date": FDue,
	"备注": FNote, "note": FNote, "notes": FNote, "说明": FNote, "描述": FNote, "desc": FNote, "description": FNote,
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "_", "", "-", "", "\t", "",
		"（", "", "）", "", "(", "", ")", "").Replace(s)
}

func canonField(col string) string {
	if f, ok := fieldAliases[norm(col)]; ok {
		return f
	}
	return ""
}

// ---------- 状态 ----------

// 四种状态的规范值 —— 写进 md 的就是这些 emoji
const (
	stDone   = "✅"
	stDoing  = "⏸\ufe0f" // 带变体选择符，才能渲染成彩色图标
	stHold   = "❌"
	stCancel = "\U0001f534"
)

// StatusDef 一种状态。Key 是写入 md 的规范值（emoji），Label 是给终端看的中文名。
type StatusDef struct {
	Key    string
	Label  string
	Closed bool // 结束态：可被归档
	Rank   int  // 排序权重，越小越靠前
	Alias  []string
}

var statusDefs = []StatusDef{
	{
		Key: stDone, Label: "完成", Closed: true, Rank: 3,
		Alias: []string{"done", "finished", "finish", "complete", "完成", "已完成", "完"},
	},
	{
		Key: stDoing, Label: "进行中", Closed: false, Rank: 1,
		Alias: []string{"doing", "wip", "进行中", "进行", "在做"},
	},
	{
		Key: stHold, Label: "停滞", Closed: false, Rank: 2,
		Alias: []string{"hold", "stuck", "blocked", "pause", "停滞", "暂停", "搁置", "卡住"},
	},
	{
		Key: stCancel, Label: "取消", Closed: true, Rank: 3,
		Alias: []string{"cancel", "canceled", "cancelled", "drop", "abort", "取消", "放弃", "已取消"},
	},
}

// stripVS 去掉变体选择符，好让 "⏸️" 和 "⏸" 都能匹配
func stripVS(s string) string {
	return strings.NewReplacer("\ufe0e", "", "\ufe0f", "").Replace(s)
}

func (d *StatusDef) matches(v string) bool {
	k := strings.ToLower(stripVS(strings.TrimSpace(v)))
	if k == "" {
		return false
	}
	if k == strings.ToLower(stripVS(d.Key)) || k == strings.ToLower(d.Label) {
		return true
	}
	for _, a := range d.Alias {
		if k == strings.ToLower(stripVS(a)) {
			return true
		}
	}
	return false
}

func statusDefOf(v string) *StatusDef {
	for i := range statusDefs {
		if statusDefs[i].matches(v) {
			return &statusDefs[i]
		}
	}
	return nil
}

// isClosedStatus 完成 / 取消：可以归档
func isClosedStatus(v string) bool {
	if d := statusDefOf(v); d != nil {
		return d.Closed
	}
	return false
}

// statusRankOf 空状态（待办）排最前，未知状态按活跃处理
func statusRankOf(v string) int {
	if strings.TrimSpace(v) == "" {
		return 0
	}
	if d := statusDefOf(v); d != nil {
		return d.Rank
	}
	return 1
}

// canonicalStatus 把各种写法归一到 Key，认不出来就原样返回
func canonicalStatus(v string) string {
	if d := statusDefOf(v); d != nil {
		return d.Key
	}
	return strings.TrimSpace(v)
}

// ---------- Task ----------

// Task 一行任务。不认识的列原样存在 Extra 里，写回时不会丢。
type Task struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Priority string            `json:"priority"`
	Due      string            `json:"due"`
	Note     string            `json:"note"`
	Extra    map[string]string `json:"extra,omitempty"`
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
	}
	if t.Extra == nil {
		t.Extra = map[string]string{}
	}
	t.Extra[col] = v
}

func (t *Task) isBlank() bool {
	if strings.TrimSpace(t.ID+t.Title+t.Status+t.Priority+t.Due+t.Note) != "" {
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

type tbl struct {
	start, end int
	Columns    []string
	fields     []string
	tasks      []Task
}

func (tb *tbl) parse(lines []string) (dirty bool) {
	// 表头
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

	// 数据行
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
	rows = append(rows, "| "+strings.Join(sep, " | ")+" |")
	for i := range tb.tasks {
		vals := make([]string, len(tb.Columns))
		for j, col := range tb.Columns {
			vals[j] = escapeCell(tb.tasks[i].Get(col))
		}
		rows = append(rows, "| "+strings.Join(vals, " | ")+" |")
	}
	return rows
}

// ---------- Store ----------

// Store 以 md 文件为唯一数据源。表格之外的所有内容（标题、说明、其它段落）都会原样保留。
type Store struct {
	mu     sync.Mutex
	path   string
	backup bool

	crlf      bool
	dirty     bool
	lines     []string
	main      *tbl
	arch      *tbl
	archTitle string // 归档章节标题
}

func NewStore(path string, backup bool) *Store {
	title := os.Getenv("MDTASK_ARCHIVE_HEADING")
	if title == "" {
		title = "归档"
	}
	return &Store{path: path, backup: backup, archTitle: title}
}

// Init 首次加载，按需补齐缺失列 / 编号并落盘。
func (s *Store) Init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	if s.dirty {
		return s.flush()
	}
	return nil
}

func (s *Store) Load() error {
	s.dirty = false
	b, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		b = []byte(defaultDoc())
		s.dirty = true // 文件不存在，初始化时落盘生成
	}
	raw := string(b)
	s.crlf = strings.Count(raw, "\r\n") > 0
	s.lines = strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	s.main, s.arch = nil, nil

	a, b2 := locateTableFrom(s.lines, 0)
	if a < 0 {
		s.createMainTable()
		a, b2 = locateTableFrom(s.lines, 0)
	}
	s.main = &tbl{start: a, end: b2}
	if s.main.parse(s.lines) {
		s.dirty = true
	}

	// 归档章节（必须在主表之后）
	if hi := findHeadingFrom(s.lines, s.archTitle, s.main.end); hi >= 0 {
		if x, y := locateTableFrom(s.lines, hi+1); x >= 0 {
			s.arch = &tbl{start: x, end: y}
			if s.arch.parse(s.lines) {
				s.dirty = true
			}
		}
	}
	return nil
}

// List 每次都从磁盘重新加载，这样用编辑器手改 md 后程序能立刻看到。
func (s *Store) List() ([]Task, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, nil, err
	}
	return cloneTasks(s.main.tasks), append([]string(nil), s.main.Columns...), nil
}

// ListArchive 返回归档表里的任务
func (s *Store) ListArchive() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, err
	}
	if s.arch == nil {
		return nil, nil
	}
	return cloneTasks(s.arch.tasks), nil
}

// Update 在读-改-写之间加锁；进入回调前已重新加载最新磁盘内容。
func (s *Store) Update(fn func(st *Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return s.flush()
}

// Archive 把主表里满足条件的任务搬进归档表，返回搬走的数量。
// 没有命中时不会新建归档章节，避免无谓改动文件。
func (s *Store) Archive(pred func(Task) bool) int {
	rest := make([]Task, 0, len(s.main.tasks))
	var hit []Task
	for _, t := range s.main.tasks {
		if pred(t) {
			hit = append(hit, t)
		} else {
			rest = append(rest, t)
		}
	}
	if len(hit) == 0 {
		return 0
	}
	s.ensureArchive()
	s.arch.tasks = append(s.arch.tasks, hit...)
	s.main.tasks = rest
	return len(hit)
}

// ensureArchive 没有归档章节就在文件末尾建一个
func (s *Store) ensureArchive() {
	if s.arch != nil {
		return
	}
	cols := append([]string(nil), s.main.Columns...)
	if len(cols) == 0 {
		cols = append([]string(nil), stdColumns...)
	}
	if strings.TrimSpace(s.lines[len(s.lines)-1]) != "" {
		s.lines = append(s.lines, "")
	}
	s.lines = append(s.lines, "## "+s.archTitle, "")
	start := len(s.lines)
	s.lines = append(s.lines, headerRows(cols)...)

	s.arch = &tbl{start: start, end: len(s.lines), Columns: cols}
	for _, c := range cols {
		s.arch.fields = append(s.arch.fields, canonField(c))
	}
	s.dirty = true
}

func (s *Store) nextID() string {
	max := 0
	scan := func(ts []Task) {
		for _, t := range ts {
			if n, err := strconv.Atoi(strings.TrimSpace(t.ID)); err == nil && n > max {
				max = n
			}
		}
	}
	scan(s.main.tasks)
	if s.arch != nil {
		scan(s.arch.tasks)
	}
	return strconv.Itoa(max + 1)
}

func nextFreeID(used map[string]bool) string {
	for i := 1; ; i++ {
		id := strconv.Itoa(i)
		if !used[id] {
			return id
		}
	}
}

// ---------- 扫描 ----------

func defaultDoc() string {
	return "# 任务清单\n\n" +
		"| 状态 | ID | 标题 | 优先级 | 截止日期 | 备注 |\n" +
		"|------|------|--------|--------|----------|------|\n"
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

// locateTableFrom 从 from 行开始找第一个 markdown 表格，返回 [表头行, 结束行)
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
		return i, end
	}
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
			return i
		}
	}
	return -1
}

func (s *Store) createMainTable() {
	blank := true
	for _, l := range s.lines {
		if strings.TrimSpace(l) != "" {
			blank = false
			break
		}
	}
	if blank {
		s.lines = strings.Split(strings.TrimRight(defaultDoc(), "\n"), "\n")
		return
	}
	if strings.TrimSpace(s.lines[len(s.lines)-1]) != "" {
		s.lines = append(s.lines, "")
	}
	s.lines = append(s.lines, headerRows(stdColumns)...)
	s.dirty = true
}

// ---------- 单元格 ----------

// splitRow 按未转义的 | 拆分单元格，并还原 \| 与 <br>。
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

// ---------- 写回 ----------

func (s *Store) flush() error {
	type repl struct {
		start, end int
		rows       []string
	}
	var rs []repl
	if s.main != nil {
		rs = append(rs, repl{s.main.start, s.main.end, s.main.render()})
	}
	if s.arch != nil {
		rs = append(rs, repl{s.arch.start, s.arch.end, s.arch.render()})
	}
	// start 大的先替换，这样前面的行号不受影响
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })

	out := append([]string(nil), s.lines...)
	for _, r := range rs {
		nl := make([]string, 0, len(out)+len(r.rows))
		nl = append(nl, out[:r.start]...)
		nl = append(nl, r.rows...)
		nl = append(nl, out[r.end:]...)
		out = nl
	}

	nlSep := "\n"
	if s.crlf {
		nlSep = "\r\n"
	}
	if s.backup {
		backupFile(s.path)
	}
	s.dirty = false
	return atomicWrite(s.path, []byte(strings.Join(out, nlSep)))
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mdtask-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	os.Chmod(name, 0o644)
	if err := os.Rename(name, path); err != nil {
		// Windows 上目标已存在时 rename 会失败，先删再改
		if rmErr := os.Remove(path); rmErr == nil {
			if err2 := os.Rename(name, path); err2 == nil {
				return nil
			}
		}
		os.Remove(name)
		return err
	}
	return nil
}

// backupFile 写前备份到 .mdtask-backup/，只保留最近 10 份。
func backupFile(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	dir := filepath.Join(filepath.Dir(path), ".mdtask-backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := time.Now().Format("20060102-150405.000") + ".md"
	os.WriteFile(filepath.Join(dir, name), src, 0o644)

	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) <= 10 {
		return
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-10] {
		os.Remove(filepath.Join(dir, n))
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

type errNotFound struct{ id string }

func (e errNotFound) Error() string { return "任务不存在: " + e.id }
