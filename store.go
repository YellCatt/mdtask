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

// createMainTable 文件里没有表格时，在末尾补一个空表格
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

// atomicWrite 先写临时文件再改名，避免写一半崩掉毁掉原文件
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

type errNotFound struct{ id string }

func (e errNotFound) Error() string { return "任务不存在: " + e.id }
