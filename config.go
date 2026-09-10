package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// config.yaml 的结构
type Config struct {
	File   string `yaml:"file"`
	Backup bool   `yaml:"backup"`
	Color  string `yaml:"color"` // auto / always / never

	Archive struct {
		Heading      string `yaml:"heading"`
		Auto         int    `yaml:"auto"`          // -1 关闭，0 立刻归档，N 天后归档
		IncludeStuck bool   `yaml:"include_stuck"` // 归档时连停滞一起搬
	} `yaml:"archive"`

	Report struct {
		Times    []string `yaml:"times"`
		Interval int      `yaml:"interval"`
		Open     bool     `yaml:"open"`
		Notify   bool     `yaml:"notify"`
		Dir      string   `yaml:"dir"`
	} `yaml:"report"`
}

const configHelp = `# MDTask 配置
# 放在 mdtask 同目录，或运行时用 -config 指定。命令行参数优先级高于这里。

# 任务 md 文件（相对路径按当前目录解析）
file: tasks.md

# 每次写入前把旧文件备份到 .mdtask-backup/（保留最近 10 份）
backup: true

# 颜色: auto（跟随终端）/ always / never
color: auto

# 四种状态用的图标，改这里就能换整套图标
status:
  done: "✅"
  doing: "⏸️"
  hold: "❌"
  cancel: "🔴"

archive:
  # 归档章节的标题，程序按这个标题找第二个表格
  heading: 归档
  # 自动归档: -1 关闭 / 0 一标记结束就归档 / 7 截止日期 7 天前才归档
  auto: -1
  # mdtask archive 时是否连「停滞」一起搬走
  include_stuck: false

report:
  # 每天出日报的时间点
  times:
    - "21:00"
    - "05:00"
  # daemon 扫描 md 变化的间隔（秒）
  interval: 60
  # 生成日报后用默认程序打开
  open: false
  # 弹系统通知（Windows 气泡 / Linux notify-send / macOS）
  notify: true
  # 日报存放目录（相对 md 文件所在目录）
  dir: .mdtask-daily
`

func defaultConfig() *Config {
	c := &Config{File: "tasks.md", Backup: true, Color: "auto"}
	c.Archive.Heading = "归档"
	c.Archive.Auto = -1
	c.Report.Times = []string{"21:00", "05:00"}
	c.Report.Interval = 60
	c.Report.Notify = true
	c.Report.Dir = ".mdtask-daily"
	return c
}

var cfg = defaultConfig()

// cfgPathUsed 实际生效的配置文件路径，install 时要写进启动命令
var cfgPathUsed string

// loadConfig 读取配置；explicit 为空时依次找 ./config.yaml、./config.yml
func loadConfig(explicit string) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	} else {
		candidates = append(candidates, "config.yaml", "config.yml")
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			if explicit != "" {
				fatal(fmt.Errorf("读不到配置文件 %s: %w", p, err))
			}
			continue
		}
		root, err := parseYAML(string(b))
		if err != nil {
			fatal(fmt.Errorf("配置文件 %s 解析失败: %w", p, err))
		}
		applyConfig(cfg, root)
		if abs, err := filepath.Abs(p); err == nil {
			cfgPathUsed = abs
		} else {
			cfgPathUsed = p
		}
		return
	}
}

func applyConfig(c *Config, n *yamlNode) {
	if v, ok := n.val("file"); ok && v.str() != "" {
		c.File = v.str()
	}
	if v, ok := n.val("backup"); ok {
		if b, ok2 := v.bool(); ok2 {
			c.Backup = b
		}
	}
	if v, ok := n.val("color"); ok && v.str() != "" {
		c.Color = strings.ToLower(v.str())
	}
	if m, ok := n.val("status"); ok {
		for _, k := range []string{"done", "doing", "hold", "cancel"} {
			if v, ok2 := m.val(k); ok2 {
				if s := strings.TrimSpace(v.str()); s != "" {
					setStatusEmoji(k, s)
				}
			}
		}
	}
	if m, ok := n.val("archive"); ok {
		if v, ok2 := m.val("heading"); ok2 && v.str() != "" {
			c.Archive.Heading = v.str()
		}
		if v, ok2 := m.val("auto"); ok2 {
			if i, ok3 := v.intVal(); ok3 {
				c.Archive.Auto = i
			}
		}
		if v, ok2 := m.val("include_stuck"); ok2 {
			if b, ok3 := v.bool(); ok3 {
				c.Archive.IncludeStuck = b
			}
		}
	}
	if m, ok := n.val("report"); ok {
		if v, ok2 := m.val("times"); ok2 {
			if ss := v.strings(); len(ss) > 0 {
				c.Report.Times = ss
			}
		}
		if v, ok2 := m.val("interval"); ok2 {
			if i, ok3 := v.intVal(); ok3 && i > 0 {
				c.Report.Interval = i
			}
		}
		if v, ok2 := m.val("open"); ok2 {
			if b, ok3 := v.bool(); ok3 {
				c.Report.Open = b
			}
		}
		if v, ok2 := m.val("notify"); ok2 {
			if b, ok3 := v.bool(); ok3 {
				c.Report.Notify = b
			}
		}
		if v, ok2 := m.val("dir"); ok2 && v.str() != "" {
			c.Report.Dir = v.str()
		}
	}
}

var statusIndex = map[string]int{"done": 0, "doing": 1, "hold": 2, "cancel": 3}

// setStatusEmoji 换图标，同时把旧图标留作别名，免得老数据认不出来
func setStatusEmoji(name, emoji string) {
	i, ok := statusIndex[name]
	if !ok {
		return
	}
	old := statusDefs[i].Key
	if old == emoji {
		return
	}
	statusDefs[i].Key = emoji
	statusDefs[i].Alias = append(statusDefs[i].Alias, old)
}

// cmdInit 在当前目录生成一份带注释的默认 config.yaml
func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	force := fs.Bool("force", false, "已存在时覆盖")
	fs.Parse(args)

	path := "config.yaml"
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Printf("%s 已存在，没动它（要覆盖加 -force）\n", path)
		return
	}
	if err := os.WriteFile(path, []byte(configHelp), 0o644); err != nil {
		fatal(err)
	}
	abs, _ := filepath.Abs(path)
	fmt.Printf("已生成 %s\n", abs)
}
