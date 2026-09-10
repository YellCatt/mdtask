package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------- 指标 ----------

type metric struct {
	key  string // 对应 daemonEvent.Kind
	name string // 报告里显示的名字
	unit string // 单位
	icon string // md 小节图标
}

var reportMetrics = []metric{
	{evNew, "新增任务", "项", "🆕"},
	{evDone, "完成任务", "项", "✅"},
	{evCancel, "取消任务", "项", "🔴"},
	{evRemoved, "删除任务", "项", "🗑️"},
}

// ---------- 统计周期 ----------

type periodKind int

const (
	kDaily periodKind = iota
	kWeekly
	kMonthly
	kYearly
)

var weekCN = []string{"日", "一", "二", "三", "四", "五", "六"}

func parsePeriodKind(s string) (periodKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "d", "day", "daily", "日报", "日":
		return kDaily, true
	case "w", "week", "weekly", "周报", "周":
		return kWeekly, true
	case "m", "month", "monthly", "月报", "月":
		return kMonthly, true
	case "y", "year", "yearly", "annual", "年报", "年":
		return kYearly, true
	}
	return kDaily, false
}

// period 一个统计周期：start 闭、end 开，中间切成若干桶用来画柱状图
type period struct {
	kind    periodKind
	start   time.Time
	end     time.Time
	buckets []time.Time
	labels  []string // 图里每行行首，如 "09-09 周三 16:00"
	shorts  []string // 高峰/低峰里用的短标签，如 "16:00"
	gran    string   // 粒度名
	title   string   // 标题里的日期段
	scope   string   // 统计窗口文案
	sumName string
	incName string
	rptName string
	file    string // 落盘文件名
	key     string // 去重用的周期标识
}

func startOfWeek(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	d := t.AddDate(0, 0, -(wd - 1))
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
}

// newPeriod ref 所在的那个周期；prev=true 则往前推一个周期（上周 / 上月 / 去年）
func newPeriod(kind periodKind, ref time.Time, prev bool) period {
	ref = time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, ref.Location())
	p := period{kind: kind}
	dayLabel := func(t time.Time) string {
		return fmt.Sprintf("%s 周%s", t.Format("01-02"), weekCN[t.Weekday()])
	}

	switch kind {
	case kDaily:
		if prev {
			ref = ref.AddDate(0, 0, -1)
		}
		p.start, p.end = ref, ref.AddDate(0, 0, 1)
		p.gran, p.sumName, p.incName, p.rptName = "按小时", "当日汇总", "当日增量", "日活跃度报告"
		p.title = ref.Format("2006-01-02")
		p.scope = ref.Format("2006-01-02") + " 00:00 ~ " + p.end.Add(-time.Minute).Format("15:04")
		for i := 0; i < 24; i++ {
			bt := ref.Add(time.Duration(i) * time.Hour)
			p.buckets = append(p.buckets, bt)
			p.labels = append(p.labels, dayLabel(bt)+" "+bt.Format("15:04"))
			p.shorts = append(p.shorts, bt.Format("15:04"))
		}
	case kWeekly:
		if prev {
			ref = ref.AddDate(0, 0, -7)
		}
		p.start = startOfWeek(ref)
		p.end = p.start.AddDate(0, 0, 7)
		p.gran, p.sumName, p.incName, p.rptName = "按天", "本周汇总", "本周增量", "周活跃度报告"
		p.title = p.start.Format("2006-01-02") + " ~ " + p.end.AddDate(0, 0, -1).Format("2006-01-02")
		p.scope = p.title
		for i := 0; i < 7; i++ {
			bt := p.start.AddDate(0, 0, i)
			p.buckets = append(p.buckets, bt)
			p.labels = append(p.labels, dayLabel(bt))
			p.shorts = append(p.shorts, bt.Format("01-02"))
		}
	case kMonthly:
		ref = time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, ref.Location())
		if prev {
			ref = ref.AddDate(0, -1, 0)
		}
		p.start = ref
		p.end = p.start.AddDate(0, 1, 0)
		p.gran, p.sumName, p.incName, p.rptName = "按天", "本月汇总", "本月增量", "月活跃度报告"
		p.title = p.start.Format("2006-01")
		p.scope = p.start.Format("2006-01-02") + " ~ " + p.end.AddDate(0, 0, -1).Format("2006-01-02")
		for bt := p.start; bt.Before(p.end); bt = bt.AddDate(0, 0, 1) {
			p.buckets = append(p.buckets, bt)
			p.labels = append(p.labels, dayLabel(bt))
			p.shorts = append(p.shorts, bt.Format("01-02"))
		}
	case kYearly:
		ref = time.Date(ref.Year(), 1, 1, 0, 0, 0, 0, ref.Location())
		if prev {
			ref = ref.AddDate(-1, 0, 0)
		}
		p.start = ref
		p.end = p.start.AddDate(1, 0, 0)
		p.gran, p.sumName, p.incName, p.rptName = "按月", "本年汇总", "本年增量", "年活跃度报告"
		p.title = p.start.Format("2006")
		p.scope = p.start.Format("2006-01-02") + " ~ " + p.end.AddDate(0, 0, -1).Format("2006-01-02")
		for bt := p.start; bt.Before(p.end); bt = bt.AddDate(0, 1, 0) {
			p.buckets = append(p.buckets, bt)
			p.labels = append(p.labels, bt.Format("01月"))
			p.shorts = append(p.shorts, bt.Format("01月"))
		}
	}

	switch kind {
	case kWeekly:
		y, w := p.start.ISOWeek()
		p.file = fmt.Sprintf("%d-W%02d.md", y, w)
	case kMonthly:
		p.file = p.start.Format("2006-01") + ".md"
	case kYearly:
		p.file = p.start.Format("2006") + ".md"
	default:
		p.file = p.start.Format("2006-01-02") + ".md"
	}
	p.key = strings.TrimSuffix(p.file, ".md")
	return p
}

// bucketOf 时间落在第几个桶里
func (p period) bucketOf(t time.Time) int {
	if len(p.buckets) == 0 {
		return -1
	}
	lo, hi := 0, len(p.buckets)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if !p.buckets[mid].After(t) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if p.buckets[lo].After(t) {
		return -1
	}
	return lo
}

// ---------- 汇总数据 ----------

type reportData struct {
	p       period
	counts  [][]int          // [指标][桶] 该桶内的增量
	events  [][]daemonEvent  // [指标] 本期事件明细
	base    []int            // [指标] 窗口开始时的累计值
	final   []int            // [指标] 窗口结束时的累计值
	covered int              // 有增量的桶数
}

func (d *daemon) buildReport(p period) *reportData {
	rd := &reportData{p: p}
	rd.counts = make([][]int, len(reportMetrics))
	rd.events = make([][]daemonEvent, len(reportMetrics))
	rd.base = make([]int, len(reportMetrics))
	rd.final = make([]int, len(reportMetrics))
	for i := range reportMetrics {
		rd.counts[i] = make([]int, len(p.buckets))
	}

	idx := map[string]int{}
	for i, m := range reportMetrics {
		idx[m.key] = i
	}
	for _, e := range d.state.Events {
		mi, ok := idx[e.Kind]
		if !ok {
			continue
		}
		if e.At.Before(p.start) {
			rd.base[mi]++
			continue
		}
		if !e.At.Before(p.end) {
			continue
		}
		if b := p.bucketOf(e.At); b >= 0 {
			rd.counts[mi][b]++
			rd.events[mi] = append(rd.events[mi], e)
		}
	}
	for i := range reportMetrics {
		s := rd.base[i]
		for _, v := range rd.counts[i] {
			s += v
		}
		rd.final[i] = s
	}
	for b := range p.buckets {
		for i := range reportMetrics {
			if rd.counts[i][b] > 0 {
				rd.covered++
				break
			}
		}
	}
	return rd
}

// ---------- 渲染 ----------

// barOf 画一根柱子；max 为 0 时整根留空
func barOf(v, max, width int) string {
	n := 0
	if max > 0 {
		n = int(math.Round(float64(v) / float64(max) * float64(width)))
	}
	if n > width {
		n = width
	}
	if n < 0 {
		n = 0
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

const barWidth = 20

// joinSome 最多列 n 个，多了就写「等共 N 个」
func joinSome(ls []string, n int) string {
	if len(ls) <= n {
		return strings.Join(ls, " / ")
	}
	return strings.Join(ls[:n], " / ") + fmt.Sprintf(" 等共%d个", len(ls))
}

func metricVals(v []int) string {
	parts := make([]string, len(reportMetrics))
	for i, m := range reportMetrics {
		parts[i] = fmt.Sprintf("%s=%d", m.name, v[i])
	}
	return strings.Join(parts, "  ")
}

// render 生成纯文本报告（终端直接看、md 里放进代码块）
func (rd *reportData) render() string {
	p := rd.p
	var b strings.Builder

	head := fmt.Sprintf("  MDTask %s  %s", p.rptName, p.title)
	line := strings.Repeat("=", 50)
	fmt.Fprintf(&b, "%s\n%s\n%s\n\n", line, head, line)

	fmt.Fprintf(&b, "覆盖周期数(有增量): %d / %d\n\n", rd.covered, len(p.buckets))

	fmt.Fprintf(&b, "【%s】(统计窗口 %s)\n", p.sumName, p.scope)
	fmt.Fprintf(&b, "  起始基准 : %s  %s\n", p.start.Format("2006-01-02 15:04:05"), metricVals(rd.base))
	fmt.Fprintf(&b, "  期末值   : %s  %s\n", p.end.Format("2006-01-02 15:04:05"), metricVals(rd.final))
	incs := make([]string, len(reportMetrics))
	for i, m := range reportMetrics {
		incs[i] = fmt.Sprintf("%s %+d", m.name, rd.final[i]-rd.base[i])
	}
	fmt.Fprintf(&b, "  %s : %s\n\n", pad(p.incName, 8), strings.Join(incs, "  "))

	fmt.Fprintf(&b, "【高峰/低峰分析】(粒度: %s)\n", p.gran)
	for i, m := range reportMetrics {
		vals := rd.counts[i]
		if len(vals) == 0 {
			fmt.Fprintf(&b, "  %s(%s) : 本期无增量\n", m.name, m.unit)
			continue
		}
		max, min := vals[0], vals[0]
		for _, v := range vals[1:] {
			if v > max {
				max = v
			}
			if v < min {
				min = v
			}
		}
		if max == 0 {
			fmt.Fprintf(&b, "  %s(%s) : 本期无增量\n", m.name, m.unit)
			continue
		}
		var peaks, lows []string
		for j, v := range vals {
			if v == max {
				peaks = append(peaks, p.shorts[j])
			}
			if v == min {
				lows = append(lows, p.shorts[j])
			}
		}
		fmt.Fprintf(&b, "  %s(%s) : 高峰 %s (+%d) | 低峰 %s (+%d)\n",
			m.name, m.unit, joinSome(peaks, 3), max, joinSome(lows, 3), min)
	}
	fmt.Fprintln(&b)

	labelW := 0
	for _, l := range p.labels {
		if w := dispWidth(l); w > labelW {
			labelW = w
		}
	}
	for i, m := range reportMetrics {
		fmt.Fprintf(&b, "【%s】(%s)\n", m.name, m.unit)
		max := 0
		for _, v := range rd.counts[i] {
			if v > max {
				max = v
			}
		}
		for j := range p.buckets {
			v := rd.counts[i][j]
			row := fmt.Sprintf("  %s %s %s", pad(p.labels[j], labelW), barOf(v, max, barWidth), fmt.Sprintf("%10d", v))
			if max > 0 && v == max {
				row += " ⚠️"
			}
			fmt.Fprintln(&b, row)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// details 本期事件明细，md 格式
func (rd *reportData) details() string {
	var b strings.Builder
	for i, m := range reportMetrics {
		es := rd.events[i]
		fmt.Fprintf(&b, "## %s %s（%d）\n\n", m.icon, m.name, len(es))
		if len(es) == 0 {
			fmt.Fprintf(&b, "本期没有%s\n\n", m.name)
			continue
		}
		for _, e := range es {
			title := strings.TrimSpace(e.Title)
			if title == "" {
				title = "（已删除）"
			}
			fmt.Fprintf(&b, "- #%s %s  `%s`\n", e.ID, title, e.At.Format("01-02 15:04"))
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

// snapshotMD 当前任务快照（进行中 / 停滞 / 待办 / 逾期）
func snapshotMD() string {
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
	fmt.Fprintf(&b, "## 当前状态\n\n")
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
	return b.String()
}

// ---------- 生成 ----------

// reportPeriod 出一份报告：打印到终端、写进 md、可选弹通知
func (d *daemon) reportPeriod(kind periodKind, ref time.Time, prev, notifyIt, openIt bool) string {
	p := newPeriod(kind, ref, prev)
	rd := d.buildReport(p)

	text := rd.render()
	fmt.Print(text)

	dir := d.dailyDir()
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, p.file)

	content := fmt.Sprintf("# MDTask %s  %s\n\n```text\n%s```\n\n%s%s",
		p.rptName, p.title, strings.TrimRight(text, "\n")+"\n", rd.details(), snapshotMD())
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		content = string(old) + "\n---\n\n" + content
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logf("写报告失败: %v", err)
		return path
	}

	if kind == kDaily {
		d.state.LastReport = ref
		d.state.LastFire = ref.Format("2006-01-02T15:04")
		d.saveState()
	}

	summary := fmt.Sprintf("新增 %+d · 完成 %+d · 取消 %+d · 删除 %+d",
		rd.final[0]-rd.base[0], rd.final[1]-rd.base[1],
		rd.final[2]-rd.base[2], rd.final[3]-rd.base[3])

	logf("%s已生成: %s", p.rptName, path)
	logf(summary)
	if notifyIt {
		notify("MDTask "+p.rptName+" "+p.title, summary+"\n"+path)
	}
	if openIt {
		openFile(path)
	}
	return path
}

// reportScheduled 到点时顺带看看周报 / 月报 / 年报是不是也该出了
func (d *daemon) reportScheduled(now time.Time) {
	if cfg.Report.Weekly > 0 && isoWeekday(now) == cfg.Report.Weekly {
		key := newPeriod(kWeekly, now, true).key
		if d.state.LastWeekly != key {
			d.reportPeriod(kWeekly, now, true, cfg.Report.Notify, d.openFile)
			d.state.LastWeekly = key
			d.saveState()
		}
	}
	if cfg.Report.Monthly > 0 && now.Day() == cfg.Report.Monthly {
		key := newPeriod(kMonthly, now, true).key
		if d.state.LastMonthly != key {
			d.reportPeriod(kMonthly, now, true, cfg.Report.Notify, d.openFile)
			d.state.LastMonthly = key
			d.saveState()
		}
	}
	if y := strings.TrimSpace(cfg.Report.Yearly); y != "" && now.Format("01-02") == y {
		key := newPeriod(kYearly, now, true).key
		if d.state.LastYearly != key {
			d.reportPeriod(kYearly, now, true, cfg.Report.Notify, d.openFile)
			d.state.LastYearly = key
			d.saveState()
		}
	}
}

func isoWeekday(t time.Time) int {
	w := int(t.Weekday())
	if w == 0 {
		return 7
	}
	return w
}

func weeklyDesc() string {
	if cfg.Report.Weekly <= 0 || cfg.Report.Weekly > 7 {
		return "关"
	}
	return "周" + weekCN[cfg.Report.Weekly%7]
}

func monthlyDesc() string {
	if cfg.Report.Monthly <= 0 {
		return "关"
	}
	return fmt.Sprintf("每月%d号", cfg.Report.Monthly)
}

func yearlyDesc() string {
	if strings.TrimSpace(cfg.Report.Yearly) == "" {
		return "关"
	}
	return strings.TrimSpace(cfg.Report.Yearly)
}

func dueSuffix(t Task) string {
	d := strings.TrimSpace(t.Due)
	if d == "" {
		return ""
	}
	return "（截止 " + d + "）"
}

// ---------- 命令 ----------

func cmdReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	kind := fs.String("type", "daily", "daily / weekly / monthly / yearly")
	date := fs.String("date", "", "参考日期 YYYY-MM-DD，默认今天")
	prev := fs.Bool("last", false, "统计上一个周期（昨天 / 上周 / 上月 / 去年）")
	noNotify := fs.Bool("no-notify", false, "不弹系统通知")
	openIt := fs.Bool("open", cfg.Report.Open, "生成后用默认程序打开")
	fs.Parse(args)

	raw := *kind
	if fs.NArg() > 0 {
		raw = fs.Arg(0)
	}
	k, ok := parsePeriodKind(raw)
	if !ok {
		fmt.Fprintf(os.Stderr, "未知报告类型: %s（daily / weekly / monthly / yearly）\n", raw)
		os.Exit(2)
	}

	ref := time.Now()
	if s := strings.TrimSpace(*date); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			fatal(fmt.Errorf("日期应为 YYYY-MM-DD: %w", err))
		}
		ref = t
	}

	d := &daemon{openFile: *openIt}
	d.loadState()
	d.poll() // 先把当前变化记进事件里
	d.reportPeriod(k, ref, *prev, !*noNotify && cfg.Report.Notify, *openIt)
}
