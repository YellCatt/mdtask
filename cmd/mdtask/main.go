// Package main 是 MDTask 守护进程的入口。
//
// MDTask 是一个基于 Markdown 表格的轻量级任务管理工具。
// 守护进程启动后会完成以下工作：
//  1. 解析命令行参数、加载 YAML 配置；
//  2. 初始化日志系统（按天轮转，保留最近 7 天）；
//  3. 扫描 tasks 目录下所有 .md 文件，建立已知任务 ID 集合；
//  4. 启动时立即生成一次四周期（日/周/月/年）报告到磁盘；
//  5. 启动一个独立 goroutine 按配置时间点发送邮件报告；
//  6. 主循环每 10s 扫描一次，为新出现的任务自动补填「添加日期」字段。
//
// 整个进程不退出（除非 fatal 或被 kill），长期驻留后台运行。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mdtask/internal/config"
	"mdtask/internal/logger"
	"mdtask/internal/mail"
	"mdtask/internal/report"
	"mdtask/internal/store"
	"mdtask/internal/ui"
)

func main() {
	// 强制把时区设为东八区 (CST)，避免运行环境时区不一致导致日期/时间计算错乱。
	// 硬编码比依赖系统时区更可靠，因为报告触发点和日期分割都跟本地日界线严格绑定。
	time.Local = time.FixedZone("CST", 8*3600)

	// 从 os.Args 中解析 -file / -config / -no-backup 等参数，
	// 返回任务目录、配置文件路径、是否禁用备份，以及剩余未识别的参数。
	file, configPath, noBackup, _ := splitArgs(os.Args[1:])

	// 加载 YAML 配置。Load 内部会先使用 Default() 填充默认值，
	// 如果没找到配置文件还会自动生成一份默认的 config.yaml。
	cfg, _, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	// 任务目录的优先级：命令行 -file > 环境变量 MDTASK_FILE > 配置文件 cfg.Dir
	if file == "" {
		file = os.Getenv("MDTASK_FILE")
	}
	if file == "" {
		file = cfg.Dir
	}
	// 转绝对路径，后续日志里要打出来方便排查。
	abs, err := filepath.Abs(file)
	if err != nil {
		fatal(err)
	}
	// 保证任务目录存在；没有 md 文件时 Load 内部会自动创建默认的 tasks.md。
	if err := os.MkdirAll(abs, 0o755); err != nil {
		fatal(err)
	}

	// 程序运行根目录：logs/ 和 reports/ 都建在这里，而不是任务 md 目录。
	// 优先用进程工作目录（startup.sh 会先 cd 到程序目录再启动，此时二者相同）；
	// 取不到再回退到可执行文件所在目录，最后兜底为当前目录。
	root, err := os.Getwd()
	if err != nil || root == "" {
		if exe, e2 := os.Executable(); e2 == nil {
			root = filepath.Dir(exe)
		} else {
			root = "."
		}
	}
	if rootAbs, e3 := filepath.Abs(root); e3 == nil {
		root = rootAbs
	}

	// 初始化日志：同时输出到 stderr (Error 以上) 和文件 (Debug 以上)。
	// Init 失败不致命，降级为只在 stderr 输出 Error 级别。
	if err := logger.Init(root); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 日志初始化失败: %v\n", err)
	}

	logger.Info("MDTask 启动",
		"dir", abs,
		"root", root,
		"config_path", configPath,
		"backup", !noBackup && cfg.Backup,
	)
	logger.Debug("当前配置摘要",
		"report_times", cfg.Report.Times,
		"report_interval", cfg.Report.Interval,
		"weekly", cfg.Report.Weekly,
		"monthly", cfg.Report.Monthly,
		"yearly", cfg.Report.Yearly,
		"mail_host", cfg.Mail.SMTPHost,
		"mail_port", cfg.Mail.SMTPPort,
		"mail_from", cfg.Mail.FromEmail,
	)

	// 初始化终端颜色（ui 包根据 cfg.Color 决定是否启用 ANSI 转义），
	// 并构造 Store —— 任务持久化层，内部用 sync.Mutex 保护并发读写。
	// 备份开关：配置允许且命令行没指定 -no-backup 时才开启。
	ui.InitColor(cfg.Color)
	st := store.NewStore(abs, cfg.Backup && !noBackup)

	// CollectKnownIDs 预扫描一次所有 md 文件的主表和归档表，
	// 把所有已存在的任务 ID 放进集合，作为后续 TouchAddedDates 的基线。
	// 这样新添加的任务（ID 不在集合里）就会被识别出来并自动补填添加日期。
	known, err := st.CollectKnownIDs()
	if err != nil {
		logger.Error("初始化 CollectKnownIDs 失败", "err", err)
		fatal(fmt.Errorf("初始化失败: %w", err))
	}
	logger.Info("初始化完成", "known_tasks", len(known))

	// 启动时立即生成一次四周期报告到磁盘（不管时间对不对）。
	// 这让用户刚启动就能看到报告文件，也方便调试报告格式。
	// 报告写到程序运行根目录下的 reports/，和 logs/ 同级。
	dumpAllReports(st, root)

	// 邮件调度 goroutine：独立于主循环运行，按配置的 times 发送邮件。
	logger.Info("启动邮件调度 goroutine")
	go runMailer(st, cfg)

	// 主循环：每 10s 全量扫描一次 md 文件。
	// TouchAddedDates 会重新 Load() 所有文件，所以它天然能感知外部编辑（比如用户手动改了 md）。
	logger.Info("进入主循环: 每 10s 扫描 tasks 目录")
	for {
		time.Sleep(10 * time.Second)
		known, err = st.TouchAddedDates(known)
		if err != nil {
			logger.Error("TouchAddedDates 扫描出错", "err", err)
		}
	}
}

// sentTracker 记录四种周期报告上一次成功发送的「周期 key」。
//
// 周期 key 是一个字符串：
//   - daily   -> "2026-09-10"      (当天日期)
//   - weekly  -> "2026-09-07"      (本周一的日期)
//   - monthly -> "2026-09"         (当月年月)
//   - yearly  -> "2026"            (当年年份)
//
// 每次触发发送时，用 periodKey(now, freq, cfg) 算出「当前周期 key」，
// 如果它和 tracker 里存的不一样，说明这是一个新周期，应该发送。
// 发送成功后把 tracker 里的值更新为当前 key，防止同一周期重复发送。
//
// 这个设计的好处是：即使进程中途 crash 又重启，
// initSentTracker 会根据当前时间正确恢复 tracker 的状态，
// 不会因为重启而漏发或重发。
type sentTracker struct {
	dailyKey   string
	weeklyKey  string
	monthlyKey string
	yearlyKey  string
}

// dumpAllReports 启动时一次性生成四周期报告，写入 baseDir/reports/{daily,week,month,year}/ 目录。
// baseDir 传的是程序运行根目录（和 logs/ 同级），不是任务 md 目录。
//
// 文件名规则：
//   - daily/2026-09-09.txt   （日报报告的是「昨天」的完成情况，所以用昨日日期）
//   - week/2026-09-07_2026-09-13.txt
//   - month/2026-08.txt      （月报报告的是「上个月」，所以用上月的年月）
//   - year/2025.txt          （年报同上）
//
// 为什么日报/月报/年报用「上一周期」？因为报告的是「已完成的任务」，
// 如果用当前周期，那周期还没结束，已完成的任务只是部分数据。
func dumpAllReports(st *store.Store, baseDir string) {
	logger.Info("dumpAllReports: 启动时生成四份报告")
	today := store.Today()
	root := filepath.Join(baseDir, "reports")

	// 读取所有归档任务和进行中任务；
	// BuildDaily/BuildWeekly/BuildMonthly/BuildYearly 都需要这两份数据。
	archived, _ := st.ListArchive()
	open, _, _ := st.List()
	logger.Debug("dumpAllReports 读取数据完成",
		"open", len(open),
		"archived", len(archived),
	)

	// entry 把「报告频率」、「输出文件名」、「构建出的 subject/body」打包在一起，
	// 后面统一遍历写入文件，避免写四次重复的错误处理逻辑。
	type entry struct {
		dir      string
		filename string
		subject  string
		body     string
		err      error
	}

	results := []entry{}

	// 日报：使用昨日日期，因为日报总结的是「昨天完成了什么」
	subject, body, err := report.BuildDaily(archived, open)
	results = append(results, entry{"daily", today.AddDate(0, 0, -1).Format("2006-01-02") + ".txt", subject, body, err})

	// 周报：用本周一到本周日的区间做文件名
	subject, body, err = report.BuildWeekly(archived, open)
	results = append(results, entry{"week", weekLabel(today) + ".txt", subject, body, err})

	// 月报：用上月的年月
	subject, body, err = report.BuildMonthly(archived, open)
	results = append(results, entry{"month", monthLabel(today) + ".txt", subject, body, err})

	// 年报：用去年的年份
	subject, body, err = report.BuildYearly(archived, open)
	results = append(results, entry{"year", yearLabel(today) + ".txt", subject, body, err})

	// 统一写入文件。每个周期独立 try/catch，一个失败不影响其他。
	for _, r := range results {
		fullDir := filepath.Join(root, r.dir)
		if err := os.MkdirAll(fullDir, 0o755); err != nil {
			logger.Error("创建报告目录失败", "dir", fullDir, "err", err)
			continue
		}
		fullPath := filepath.Join(fullDir, r.filename)
		if r.err != nil {
			logger.Error("生成报告失败", "freq", r.dir, "err", r.err)
			continue
		}
		content := r.subject + "\n\n" + r.body
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			logger.Error("写入报告文件失败", "path", fullPath, "err", err)
		} else {
			logger.Info("报告已生成", "path", fullPath)
		}
	}
}

// weekLabel 把 t 所在的自然周格式化为 "2026-09-07_2026-09-13" 形式。
// 周一为一周的第一天；周日(0) 会被当成 7 处理。
func weekLabel(t time.Time) string {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := t.AddDate(0, 0, 1-wd)
	sunday := monday.AddDate(0, 0, 6)
	return monday.Format("2006-01-02") + "_" + sunday.Format("2006-01-02")
}

// monthLabel 返回「上个月」的年月字符串，比如 2026-09 -> "2026-08"。
// 月报总结的是上个月的完成情况，所以需要上月的 key。
func monthLabel(t time.Time) string {
	firstOfThisMonth := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	lastOfLastMonth := firstOfThisMonth.AddDate(0, 0, -1)
	return lastOfLastMonth.Format("2006-01")
}

// yearLabel 返回「去年」的年份字符串，比如 2026 -> "2025"。
// 年报总结的是去年的完成情况。
func yearLabel(t time.Time) string {
	return fmt.Sprintf("%d", t.Year()-1)
}

// periodKey 根据当前时间 now、报告频率 freq 和配置，
// 计算出「当前周期的唯一标识字符串」。
//
// 这个 key 会被 sentTracker 用来判断一个周期是否已经发过邮件。
func periodKey(now time.Time, freq string, cfg *config.Config) string {
	switch freq {
	case "daily":
		return now.Format("2006-01-02")
	case "weekly":
		// 计算 now 所在周的周一日期
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := now.AddDate(0, 0, 1-wd)
		return monday.Format("2006-01-02")
	case "monthly":
		return now.Format("2006-01")
	case "yearly":
		return now.Format("2006")
	}
	return ""
}

// initSentTracker 在进程启动时初始化 sentTracker，
// 让它「看起来像是已经在上一个触发点运行过」，这样就不会在启动时重复发送上一周期的报告。
//
// 关键逻辑：
//   - dailyKey：如果当前时间还没到当天的触发点，说明「今天还没发」，
//     tracker 应该保持为昨天的 key，这样当今天触发时会被识别为新周期。
//     如果已经过了触发点，说明今天应该发过了，tracker 设为今天。
//   - weeklyKey：根据 cfg.Report.Weekly 计算本周的目标星期几；
//     如果今天已经过了目标日或者正好是目标日且时间已过触发点，设为本周一，否则设为上周一。
//   - monthlyKey：同理根据 cfg.Report.Monthly（目标日期号数）判断。
//   - yearlyKey：同理根据 cfg.Report.Yearly（目标 MM-DD）判断。
func initSentTracker(cfg *config.Config, t time.Time) *sentTracker {
	st := &sentTracker{}

	// reportHourMin 取配置里最后一个时间点（默认 05:00）作为「基准触发时间」。
	// 我们只需要知道「现在是在当天触发点之前还是之后」，具体发几个时间点不影响 tracker。
	hour, min := reportHourMin(cfg)
	todayTriggered := false
	trigger := time.Date(t.Year(), t.Month(), t.Day(), hour, min, 0, 0, t.Location())
	if !t.Before(trigger) {
		todayTriggered = true
	}

	// ----- daily -----
	st.dailyKey = t.Format("2006-01-02")
	if !todayTriggered {
		// 还没到今天的触发点，说明「今天还没发」，
		// tracker 保持为昨天，下次 runMailer 触发时 periodKey 算出今天会 != 昨天 -> 触发发送。
		st.dailyKey = t.AddDate(0, 0, -1).Format("2006-01-02")
	}

	// ----- weekly -----
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := t.AddDate(0, 0, 1-wd)
	st.weeklyKey = monday.Format("2006-01-02")
	if cfg.Report.Weekly > 0 {
		targetMonWeekday := cfg.Report.Weekly
		if wd > targetMonWeekday || (wd == targetMonWeekday && todayTriggered) {
			// 今天在目标日之后（或就是目标日且时间已过），说明本周该发的已经发过了。
			st.weeklyKey = monday.Format("2006-01-02")
		} else {
			// 今天还没到本周的目标日，tracker 保持为上周一，
			// 等本周目标日到来时 periodKey 算出本周一 != 上周一 -> 触发发送。
			st.weeklyKey = monday.AddDate(0, 0, -7).Format("2006-01-02")
		}
	}

	// ----- monthly -----
	if cfg.Report.Monthly > 0 {
		if t.Day() > cfg.Report.Monthly || (t.Day() == cfg.Report.Monthly && todayTriggered) {
			st.monthlyKey = t.Format("2006-01")
		} else {
			prev := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, -1, 0)
			st.monthlyKey = prev.Format("2006-01")
		}
	}

	// ----- yearly -----
	if cfg.Report.Yearly != "" {
		parts := strings.SplitN(cfg.Report.Yearly, "-", 2)
		if len(parts) == 2 {
			mt, _ := time.Parse("01", parts[0])
			d, _ := time.Parse("02", parts[1])
			triggerDate := time.Date(t.Year(), mt.Month(), d.Day(), hour, min, 0, 0, t.Location())
			if !t.Before(triggerDate) {
				st.yearlyKey = t.Format("2006")
			} else {
				st.yearlyKey = t.AddDate(-1, 0, 0).Format("2006")
			}
		}
	}
	logger.Debug("initSentTracker 初始化完成",
		"dailyKey", st.dailyKey,
		"weeklyKey", st.weeklyKey,
		"monthlyKey", st.monthlyKey,
		"yearlyKey", st.yearlyKey,
	)
	return st
}

// reportHourMin 从 cfg.Report.Times 里取出最后一个时间点的时和分。
//
// 配置里的 times 是一个切片，例如 ["21:00", "05:00"]，表示一天要发多次日报。
// runMailer 的「下一次触发时间」会遍历所有 times 取最近的那个，
// 但 sentTracker 只需要知道「当天是否已经过了最晚的那个时间点」就够了，
// 所以取最后一个即可。如果 times 为空，默认 05:00。
func reportHourMin(cfg *config.Config) (int, int) {
	times := cfg.Report.Times
	if len(times) == 0 {
		return 5, 0
	}
	last := times[len(times)-1]
	parts := strings.SplitN(last, ":", 2)
	h := 5
	m := 0
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &h)
		fmt.Sscanf(parts[1], "%d", &m)
	} else if len(parts) == 1 {
		fmt.Sscanf(parts[0], "%d", &h)
	}
	return h, m
}

// nextReportTime 计算「下一次邮件发送触发时间」。
//
// 逻辑很简单：把今天的 reportHourMin 拿出来，如果还没到就用今天的，
// 已经过了就 +1 天。这里只拿了一个触发时间，意味着目前的实现是
// 「每次 runMailer 触发时一次性检查并发送所有周期的报告」，
// 而不是分别在不同时间点发送不同周期。
func nextReportTime(cfg *config.Config, from time.Time) time.Time {
	h, m := reportHourMin(cfg)
	t := time.Date(from.Year(), from.Month(), from.Day(), h, m, 0, 0, from.Location())
	if !t.After(from) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// runMailer 邮件发送调度主循环。
//
// 工作方式：
//  1. 用 initSentTracker 在启动时初始化 tracker，避免进程刚起来就重发上一个周期的报告；
//  2. 每次循环计算「下一次触发时间」，Sleep 到那个时刻；
//  3. 触发后调用 sendAllReports，由它负责判断四种周期哪些该发、哪些不该发；
//  4. sendAllReports 会更新 tracker，所以下一个循环周期判断自然就正确了。
//
// 注意：这里的「下一次触发时间」每次都只算 reportHourMin 那一个时间点。
// 如果 cfg.Report.Times 里配置了多个时间点（比如 21:00 和 05:00），
// nextReportTime 只看最后一个，意味着其他时间点会被忽略。
// 这可能是一个待改进的地方。
func runMailer(st *store.Store, cfg *config.Config) {
	tracker := initSentTracker(cfg, time.Now())
	for {
		now := time.Now()
		next := nextReportTime(cfg, now)
		wait := next.Sub(now)
		logger.Info("runMailer: 计算下次发送时间",
			"next", next.Format(time.RFC3339),
			"wait_seconds", int(wait.Seconds()),
		)
		time.Sleep(wait)

		now = time.Now()
		logger.Info("runMailer: 到达触发时间，开始发送报告", "now", now.Format(time.RFC3339))
		sendAllReports(st, cfg, tracker, now)
	}
}

// sendAllReports 核心发送逻辑：四种周期依次检查，该发的生成报告并通过邮件发出。
//
// 每个周期对应一个 task 条目，包含四个字段：
//   - freq   : 周期名，用于日志和 periodKey
//   - check  : 判断当前时刻是否应该发送这个周期的报告
//              里面会检查「cfg 是否开启」、「今天是不是目标日」、「当前周期 key 是否和 tracker 不一样」
//   - build  : 生成报告 subject + body
//   - mark   : 发送成功后把 tracker 的对应 key 更新为当前周期 key，防止同一周期重复发
//
// 整体处理顺序是按 freq 字母排序（daily < monthly < weekly < yearly），
// 排序是为了让日志输出有稳定顺序，方便排查。
func sendAllReports(st *store.Store, cfg *config.Config, tracker *sentTracker, now time.Time) {
	logger.Debug("sendAllReports 开始", "now", now.Format(time.RFC3339))

	// 读取一次归档和进行中任务，四种周期共用，避免重复 IO。
	archived, _ := st.ListArchive()
	open, _, _ := st.List()
	logger.Debug("sendAllReports 读取数据完成",
		"open", len(open),
		"archived", len(archived),
	)

	// tasks 数组定义了四种报告周期的检查+构建+标记逻辑。
	// 每一项是一个闭包，捕获了 archived/open/cfg/now/tracker 等外部变量。
	tasks := []struct {
		freq   string
		check  func(currKey string) bool
		build  func() (string, string, error)
		mark   func(currKey string)
	}{
		{
			"daily",
			// daily 只有一个条件：当前 key != tracker 里存的 key 就发。
			// daily 是每天都发，不需要额外检查「目标日」。
			func(k string) bool {
				triggered := k != tracker.dailyKey
				logger.Debug("daily 检查", "currKey", k, "tracker", tracker.dailyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildDaily(archived, open) },
			func(k string) { tracker.dailyKey = k },
		},
		{
			"weekly",
			// weekly 发之前先检查两件事：
			//   1. cfg.Report.Weekly != 0（配置里开启了）
			//   2. 今天就是 cfg.Report.Weekly 指定的那个星期几
			// 都满足了再比较周期 key。
			func(k string) bool {
				if cfg.Report.Weekly == 0 {
					logger.Debug("weekly 关闭 (cfg.Report.Weekly=0)")
					return false
				}
				wd := int(now.Weekday())
				if wd == 0 {
					wd = 7
				}
				if wd != cfg.Report.Weekly {
					logger.Debug("weekly 跳过，不是指定星期几", "today_wd", wd, "target", cfg.Report.Weekly)
					return false
				}
				triggered := k != tracker.weeklyKey
				logger.Debug("weekly 检查", "currKey", k, "tracker", tracker.weeklyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildWeekly(archived, open) },
			func(k string) { tracker.weeklyKey = k },
		},
		{
			"monthly",
			// monthly 类似 weekly，先查 cfg.Report.Monthly != 0，
			// 再查今天是不是 cfg.Report.Monthly 指定的日期号数。
			func(k string) bool {
				if cfg.Report.Monthly == 0 {
					logger.Debug("monthly 关闭 (cfg.Report.Monthly=0)")
					return false
				}
				if now.Day() != cfg.Report.Monthly {
					logger.Debug("monthly 跳过，不是指定日期", "today_day", now.Day(), "target", cfg.Report.Monthly)
					return false
				}
				triggered := k != tracker.monthlyKey
				logger.Debug("monthly 检查", "currKey", k, "tracker", tracker.monthlyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildMonthly(archived, open) },
			func(k string) { tracker.monthlyKey = k },
		},
		{
			"yearly",
			// yearly 先查 cfg.Report.Yearly != ""（格式 MM-DD），
			// 再把 now 的月日和配置值比对。
			func(k string) bool {
				if cfg.Report.Yearly == "" {
					logger.Debug("yearly 关闭 (cfg.Report.Yearly 为空)")
					return false
				}
				parts := strings.SplitN(cfg.Report.Yearly, "-", 2)
				if len(parts) != 2 {
					logger.Debug("yearly 跳过，cfg 格式错误", "yearly", cfg.Report.Yearly)
					return false
				}
				mt, _ := time.Parse("01", parts[0])
				d, _ := time.Parse("02", parts[1])
				if int(now.Month()) != int(mt.Month()) || now.Day() != d.Day() {
					logger.Debug("yearly 跳过，不是指定日期", "today", now.Format("01-02"), "target", cfg.Report.Yearly)
					return false
				}
				triggered := k != tracker.yearlyKey
				logger.Debug("yearly 检查", "currKey", k, "tracker", tracker.yearlyKey, "triggered", triggered)
				return triggered
			},
			func() (string, string, error) { return report.BuildYearly(archived, open) },
			func(k string) { tracker.yearlyKey = k },
		},
	}

	// 排序不是业务必须的，但能让日志里四种报告的输出顺序稳定，方便对比前后两次运行。
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].freq < tasks[j].freq })

	for _, t := range tasks {
		currKey := periodKey(now, t.freq, cfg)
		if !t.check(currKey) {
			continue
		}
		subject, body, err := t.build()
		if err != nil {
			logger.Error("报告生成失败", "freq", t.freq, "err", err)
			continue
		}
		logger.Debug("报告生成成功", "freq", t.freq, "subject", subject)

		// 邮件发送：成功才 mark（更新 tracker），失败保持 tracker 不变，
		// 这样下一个触发周期会再次尝试发送同一周期的报告。
		// 注意这会导致「邮件发送失败后每天重发」，如果 SMTP 一直挂着会刷屏。
		if err := mail.Send(cfg, subject, body); err != nil {
			logger.Error("邮件发送失败", "freq", t.freq, "err", err)
		} else {
			logger.Info("报告邮件已发送", "freq", t.freq, "subject", subject)
			t.mark(currKey)
		}
	}
}

// fatal 打印错误到 stderr 并以 exit code 1 退出进程。
// 同时写一条 Error 级别的日志（便于事后排查）。
func fatal(v any) {
	logger.Error("fatal 退出", "err", v)
	fmt.Fprintln(os.Stderr, "错误:", v)
	os.Exit(1)
}

// splitArgs 从 os.Args[1:] 中解析出命令行参数：
//   - -file / --file / -f          或 -file=xxx / --file=xxx / -f=xxx
//   - -config / --config / -c      或 -config=xxx / --config=xxx / -c=xxx
//   - -no-backup / --no-backup     （布尔开关）
//
// 未被识别的参数放进 rest 返回值（目前 main 里没用）。
// 支持三种风格：--long value、--long=value、-f value、-f=value。
func splitArgs(args []string) (file, config string, noBackup bool, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-file" || a == "--file" || a == "-f":
			// value 在下一个 args 里
			if i+1 < len(args) {
				file = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-file="):
			file = strings.TrimPrefix(a, "-file=")
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case strings.HasPrefix(a, "-f="):
			file = strings.TrimPrefix(a, "-f=")
		case a == "-config" || a == "--config" || a == "-c":
			if i+1 < len(args) {
				config = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config="):
			config = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			config = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			config = strings.TrimPrefix(a, "-c=")
		case a == "-no-backup" || a == "--no-backup":
			noBackup = true
		default:
			// 非已知参数，透传出去（保留未来扩展空间）
			rest = append(rest, a)
		}
	}
	return
}