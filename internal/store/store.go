// Package store 负责把 .md 里的 markdown 表格读成任务对象、提供增删改查与归档，
// 并把改动原子地写回文件。Store 是上层（daemon / mailer）的统一入口。
package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mdtask/internal/logger"
	"mdtask/internal/util"
)

var ErrNotFound = errors.New("任务不存在")

// 任务表的标准列名（中文），解析/渲染表格时以此为基准，缺失的列会自动补齐。
const (
	FID       = "ID"
	FTitle    = "标题"
	FStatus   = "状态"
	FPriority = "优先级"
	FDue      = "截止日期"
	FNote     = "备注"
	FAdded    = "添加日期"
	FDoneAt   = "完成时间"
)

var stdColumns = []string{FStatus, FID, FTitle, FPriority, FDue, FNote, FAdded}

// fieldAliases 把各种中英文列名（如 id/编号/标题/title）归一为上面的标准列名。
var fieldAliases = map[string]string{
	"id": FID, "编号": FID, "序号": FID, "no": FID,
	"标题": FTitle, "title": FTitle, "任务": FTitle, "名称": FTitle, "name": FTitle, "内容": FTitle,
	"状态": FStatus, "status": FStatus, "state": FStatus, "进度": FStatus,
	"优先级": FPriority, "priority": FPriority, "pri": FPriority, "级别": FPriority, "重要度": FPriority,
	"截止日期": FDue, "截止": FDue, "due": FDue, "duedate": FDue, "deadline": FDue, "日期": FDue, "date": FDue,
	"备注": FNote, "note": FNote, "notes": FNote, "说明": FNote, "描述": FNote, "desc": FNote, "description": FNote,
	"添加日期": FAdded, "添加": FAdded, "added": FAdded, "created": FAdded, "created_at": FAdded, "创建日期": FAdded,
	"完成时间": FDoneAt, "完成": FDoneAt, "doneat": FDoneAt, "done_at": FDoneAt, "finished": FDoneAt, "完成日期": FDoneAt,
}

// norm 把列名转小写、去空格与常见分隔符（_ - （）等），用于别名匹配。
func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer(" ", "", "_", "", "-", "", "\t", "",
		"（", "", "）", "", "(", "", ")", "").Replace(s)
}

// canonField 把任意列名（含别名）映射到标准列名，认不出返回空串。
func canonField(col string) string {
	if f, ok := fieldAliases[norm(col)]; ok {
		return f
	}
	return ""
}

type fileState struct {
	Path  string
	Name  string
	crlf  bool
	dirty bool
	lines []string
	main  *tbl
	arch  *tbl
}

type Store struct {
	mu        sync.Mutex
	Dir       string
	Backup    bool
	ArchTitle string
	files     []*fileState
	primary   *fileState
}

// NewStore 构造 Store；归档章节标题为空时依次回退到环境变量、再回退到「归档」。
func NewStore(dir string, backup bool, archTitle string) *Store {
	title := strings.TrimSpace(archTitle)
	if title == "" {
		title = os.Getenv("MDTASK_ARCHIVE_HEADING")
	}
	if title == "" {
		title = "归档"
	}
	return &Store{Dir: dir, Backup: backup, ArchTitle: title}
}

func (s *Store) Init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load 扫描目录下的所有 .md 文件，解析出主表与归档表；目录或文件不存在时自动建默认文件。
func (s *Store) Load() error {
	s.files = nil
	s.primary = nil

	logger.Debug("Store.Load 开始扫描", "dir", s.Dir)

	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("任务目录不存在，自动创建", "dir", s.Dir)
			if err := os.MkdirAll(s.Dir, 0o755); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	var mdFiles []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			mdFiles = append(mdFiles, name)
		}
	}
	sort.Strings(mdFiles)
	logger.Debug("发现 md 文件", "count", len(mdFiles), "files", mdFiles)

	if len(mdFiles) == 0 {
		primaryPath := filepath.Join(s.Dir, "tasks.md")
		f, err := s.loadFile(primaryPath)
		if err != nil {
			return err
		}
		s.files = append(s.files, f)
		s.primary = f
		logger.Info("没有 md 文件，自动创建 tasks.md", "path", primaryPath)
		return nil
	}

	for _, name := range mdFiles {
		p := filepath.Join(s.Dir, name)
		f, err := s.loadFile(p)
		if err != nil {
			return err
		}
		s.files = append(s.files, f)
	}
	s.primary = s.files[0]
	logger.Debug("Store.Load 完成", "files", len(s.files))
	return nil
}

// loadFile 读取单个 md 文件，定位主表（必要时建表头），再尝试定位归档表并解析。
func (s *Store) loadFile(path string) (*fileState, error) {
	f := &fileState{Path: path, Name: filepath.Base(path)}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		logger.Info("文件不存在，使用默认模板", "path", path)
		b = []byte(defaultDoc())
		f.dirty = true
	}
	raw := string(b)
	f.crlf = strings.Count(raw, "\r\n") > 0
	f.lines = strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")

	a, b2 := locateTableFrom(f.lines, 0)
	if a < 0 {
		logger.Info("主表未找到，自动创建表头", "file", f.Name)
		s.createMainTableIn(f)
		a, b2 = locateTableFrom(f.lines, 0)
	}
	f.main = &tbl{start: a, end: b2}
	if f.main.parse(f.lines) {
		f.dirty = true
	}
	for i := range f.main.tasks {
		f.main.tasks[i].Source = path
	}
	logger.Debug("加载主表完成", "file", f.Name, "tasks", len(f.main.tasks))

	if hi := findHeadingFrom(f.lines, s.ArchTitle, f.main.end); hi >= 0 {
		if x, y := locateTableFrom(f.lines, hi+1); x >= 0 {
			f.arch = &tbl{start: x, end: y}
			if f.arch.parse(f.lines) {
				f.dirty = true
			}
			for i := range f.arch.tasks {
				f.arch.tasks[i].Source = path
			}
			logger.Debug("加载归档表完成", "file", f.Name, "archived_tasks", len(f.arch.tasks))
		} else {
			logger.Debug("找到归档标题但未找到表格", "file", f.Name, "heading_pos", hi)
		}
	} else {
		logger.Debug("未找到归档章节", "file", f.Name)
	}
	return f, nil
}

// createMainTableIn 在文件里补一张标准列的主表（空文件用整段默认模板，否则追加表头）。
func (s *Store) createMainTableIn(f *fileState) {
	blank := true
	for _, l := range f.lines {
		if strings.TrimSpace(l) != "" {
			blank = false
			break
		}
	}
	if blank {
		f.lines = strings.Split(strings.TrimRight(defaultDoc(), "\n"), "\n")
		return
	}
	if strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
		f.lines = append(f.lines, "")
	}
	f.lines = append(f.lines, headerRows(stdColumns)...)
	f.dirty = true
}

// List 返回所有主表里的任务及其列顺序；每次调用会先重新 Load 以反映磁盘最新内容。
func (s *Store) List() ([]Task, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, nil, err
	}
	var all []Task
	var cols []string
	for _, f := range s.files {
		if f.main == nil {
			continue
		}
		all = append(all, cloneTasks(f.main.tasks)...)
		if len(cols) == 0 {
			cols = append([]string(nil), f.main.Columns...)
		}
	}
	return all, cols, nil
}

// ListArchive 返回所有归档表里的任务。
func (s *Store) ListArchive() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, err
	}
	var all []Task
	for _, f := range s.files {
		if f.arch != nil {
			all = append(all, cloneTasks(f.arch.tasks)...)
		}
	}
	return all, nil
}

// Update 在锁内重新 Load 并执行修改回调，最后把标脏的文件统一写回磁盘。
func (s *Store) Update(fn func(st *Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	var dirtyFiles int
	for _, f := range s.files {
		if f.dirty {
			dirtyFiles++
			if err := s.flushFile(f); err != nil {
				return err
			}
		}
	}
	logger.Debug("Update 完成", "dirty_files", dirtyFiles)
	return nil
}

// Archive 把满足 pred 的主表任务搬进归档表（未填完成时间的补上今天），返回归档数量。
func (s *Store) Archive(pred func(Task) bool) int {
	total := 0
	today := util.Today().Format("2006-01-02")
	for _, f := range s.files {
		if f.main == nil {
			continue
		}
		rest := make([]Task, 0, len(f.main.tasks))
		var hit []Task
		for _, t := range f.main.tasks {
			if pred(t) {
				if t.DoneAt == "" {
					t.DoneAt = today
				}
				hit = append(hit, t)
			} else {
				rest = append(rest, t)
			}
		}
		if len(hit) == 0 {
			continue
		}
		s.ensureArchiveIn(f)
		f.arch.tasks = append(f.arch.tasks, hit...)
		f.main.tasks = rest
		f.dirty = true
		total += len(hit)
		logger.Info("Archive: 归档任务", "file", f.Name, "count", len(hit), "doneAt", today)
		for _, t := range hit {
			logger.Debug("归档项", "id", t.ID, "title", t.Title)
		}
	}
	return total
}

// AddTask 往主文件（primary）追加一条任务。
func (s *Store) AddTask(t Task) error {
	if s.primary == nil {
		return errors.New("没有可用的任务文件")
	}
	t.Source = s.primary.Path
	s.primary.main.tasks = append(s.primary.main.tasks, t)
	s.primary.dirty = true
	logger.Debug("AddTask 添加任务", "id", t.ID, "title", t.Title, "status", t.Status, "file", s.primary.Name)
	return nil
}

// UpdateTask 找到指定 ID 的任务并就地修改，找不到返回 ErrNotFound。
func (s *Store) UpdateTask(id string, fn func(*Task) error) error {
	logger.Debug("UpdateTask 开始", "id", id)
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				if err := fn(&f.main.tasks[i]); err != nil {
					return err
				}
				f.main.tasks[i].Source = f.Path
				f.dirty = true
				logger.Debug("UpdateTask 更新成功", "id", id, "file", f.Name)
				return nil
			}
		}
	}
	logger.Debug("UpdateTask 未找到任务", "id", id)
	return ErrNotFound
}

// SetTaskStatus 直接改某条任务的状态列。
func (s *Store) SetTaskStatus(id, status string) error {
	logger.Debug("SetTaskStatus 开始", "id", id, "target_status", status)
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				f.main.tasks[i].Status = status
				f.dirty = true
				logger.Debug("SetTaskStatus 更新成功", "id", id, "status", status, "file", f.Name)
				return nil
			}
		}
	}
	logger.Debug("SetTaskStatus 未找到任务", "id", id)
	return ErrNotFound
}

// RemoveTask 从主表删除指定 ID 的任务。
func (s *Store) RemoveTask(id string) error {
	logger.Debug("RemoveTask 开始", "id", id)
	for _, f := range s.files {
		for i := range f.main.tasks {
			if f.main.tasks[i].ID == id {
				f.main.tasks = append(f.main.tasks[:i], f.main.tasks[i+1:]...)
				f.dirty = true
				logger.Debug("RemoveTask 删除成功", "id", id, "file", f.Name)
				return nil
			}
		}
	}
	logger.Debug("RemoveTask 未找到任务", "id", id)
	return ErrNotFound
}

// ensureArchiveIn 确保文件里有归档表：没有补一个「## 标题」+表头，并保证含「完成时间」列。
func (s *Store) ensureArchiveIn(f *fileState) {
	if f.arch != nil {
		hasDoneAt := false
		for _, c := range f.arch.Columns {
			if canonField(c) == FDoneAt {
				hasDoneAt = true
				break
			}
		}
		if !hasDoneAt {
			f.arch.Columns = append(f.arch.Columns, FDoneAt)
			f.arch.fields = append(f.arch.fields, FDoneAt)
			f.dirty = true
		}
		return
	}
	cols := append([]string(nil), f.main.Columns...)
	if len(cols) == 0 {
		cols = append([]string(nil), stdColumns...)
	}
	hasDoneAt := false
	for _, c := range cols {
		if canonField(c) == FDoneAt {
			hasDoneAt = true
			break
		}
	}
	if !hasDoneAt {
		cols = append(cols, FDoneAt)
	}
	if strings.TrimSpace(f.lines[len(f.lines)-1]) != "" {
		f.lines = append(f.lines, "")
	}
	f.lines = append(f.lines, "## "+s.ArchTitle, "")
	start := len(f.lines)
	f.lines = append(f.lines, headerRows(cols)...)

	f.arch = &tbl{start: start, end: len(f.lines), Columns: cols}
	for _, c := range cols {
		f.arch.fields = append(f.arch.fields, canonField(c))
	}
	f.dirty = true
}

// NextID 在所有主表/归档表里取最大数字 ID，返回下一个可用 ID（连续编号）。
func (s *Store) NextID() string {
	max := 0
	scan := func(ts []Task) {
		for _, t := range ts {
			if n, err := strconv.Atoi(strings.TrimSpace(t.ID)); err == nil && n > max {
				max = n
			}
		}
	}
	for _, f := range s.files {
		if f.main != nil {
			scan(f.main.tasks)
		}
		if f.arch != nil {
			scan(f.arch.tasks)
		}
	}
	logger.Debug("NextID 计算完成", "max", max, "next", max+1)
	return strconv.Itoa(max + 1)
}

// TouchAddedDates 给「还没有添加日期」的任务（ID 不为空且 Added 为空）补上今天；
// known 由 CollectKnownIDs 初始化为「已有添加日期的 ID 集合」，因此这里只会命中
// 启动时尚缺添加日期的存量任务，以及进程运行期间新增的任务。命中后把该 ID 并入 known。
// 用于 daemon 定时扫描时自动打上添加时间。
func (s *Store) TouchAddedDates(known map[string]bool) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return known, err
	}
	today := util.Today().Format("2006-01-02")
	newCount := 0
	for _, f := range s.files {
		if f.main != nil {
			for i := range f.main.tasks {
				t := &f.main.tasks[i]
				if t.ID != "" {
					if !known[t.ID] && t.Added == "" {
						t.Added = today
						f.dirty = true
						newCount++
						logger.Debug("TouchAddedDates 补填添加日期", "task_id", t.ID, "title", t.Title, "added", today)
					}
					known[t.ID] = true
				}
			}
		}
		if f.arch != nil {
			for i := range f.arch.tasks {
				t := &f.arch.tasks[i]
				if t.ID != "" {
					if !known[t.ID] && t.Added == "" {
						t.Added = today
						f.dirty = true
						newCount++
					}
					known[t.ID] = true
				}
			}
		}
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return known, err
			}
		}
	}
	if newCount > 0 {
		logger.Info("TouchAddedDates 本轮扫描", "补填了", newCount, "个新任务")
	} else {
		logger.Debug("TouchAddedDates 本轮扫描: 无新任务")
	}
	return known, nil
}

// CollectKnownIDs 收集「当前已有添加日期」的任务 ID，供 daemon 初始化时建立 known 集合；
// 注意只收录已填添加日期的 ID，未填的留待 TouchAddedDates 在扫描时补填。
func (s *Store) CollectKnownIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Load(); err != nil {
		return nil, err
	}
	// 只把「已经有添加日期」的 ID 记入 known；这样那些启动时
	// 还没有添加日期的存量任务（以及后续新增的任务）会在扫描时补填。
	known := map[string]bool{}
	for _, f := range s.files {
		if f.main != nil {
			for _, t := range f.main.tasks {
				if t.ID != "" && t.Added != "" {
					known[t.ID] = true
				}
			}
		}
		if f.arch != nil {
			for _, t := range f.arch.tasks {
				if t.ID != "" && t.Added != "" {
					known[t.ID] = true
				}
			}
		}
	}
	for _, f := range s.files {
		if f.dirty {
			if err := s.flushFile(f); err != nil {
				return nil, err
			}
		}
	}
	return known, nil
}

// flushFile 把内存里的表格重新渲染，替换原文件对应行区间；可选先备份，再原子写入。
func (s *Store) flushFile(f *fileState) error {
	type repl struct {
		start, end int
		rows       []string
	}
	var rs []repl
	if f.main != nil {
		rs = append(rs, repl{f.main.start, f.main.end, f.main.render()})
	}
	if f.arch != nil {
		rs = append(rs, repl{f.arch.start, f.arch.end, f.arch.render()})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].start > rs[j].start })

	out := append([]string(nil), f.lines...)
	for _, r := range rs {
		nl := make([]string, 0, len(out)+len(r.rows))
		nl = append(nl, out[:r.start]...)
		nl = append(nl, r.rows...)
		nl = append(nl, out[r.end:]...)
		out = nl
	}

	nlSep := "\n"
	if f.crlf {
		nlSep = "\r\n"
	}
	if s.Backup {
		backupFile(f.Path)
	}
	f.dirty = false
	logger.Debug("flushFile 原子写入", "path", f.Path, "lines", len(out))
	if err := atomicWrite(f.Path, []byte(strings.Join(out, nlSep))); err != nil {
		logger.Error("flushFile 写入失败", "path", f.Path, "err", err)
		return err
	}
	return nil
}

// atomicWrite 先写临时文件再 rename 到目标，避免写一半时文件损坏；rename 失败会尝试删除旧文件重试。
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

// backupFile 把改写前的文件复制到 .mdtask-backup/，并只保留最近 10 份备份。
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