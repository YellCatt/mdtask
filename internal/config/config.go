package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mdtask/internal/status"
)

type Config struct {
	Dir    string `yaml:"dir"`
	Backup bool   `yaml:"backup"`
	Color  string `yaml:"color"`

	Archive struct {
		Heading      string `yaml:"heading"`
		Auto         int    `yaml:"auto"`
		IncludeStuck bool   `yaml:"include_stuck"`
	} `yaml:"archive"`

	Report struct {
		Times   []string `yaml:"times"`
		Interval int      `yaml:"interval"`
		Weekly   int      `yaml:"weekly"`
		Monthly  int      `yaml:"monthly"`
		Yearly   string   `yaml:"yearly"`
	} `yaml:"report"`

	Mail struct {
		SMTPHost      string `yaml:"smtp_host"`
		SMTPPort      int    `yaml:"smtp_port"`
		FromEmail     string `yaml:"from_email"`
		AuthCode      string `yaml:"auth_code"`
		ToEmail       string `yaml:"to_email"`
		TLSSkipVerify bool   `yaml:"tls_skip_verify"`
		Timeout       int    `yaml:"timeout"`
	} `yaml:"mail"`
}

const configHelp = `# MDTask 配置
# 放在 mdtask 同目录，或运行时用 -config 指定。命令行参数优先级高于这里。

# 任务 md 文件目录（目录下所有 .md 都会被读取）
dir: tasks

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
  # 自动归档: -1 关闭 / 0 一标记结束就归档（默认）/ N 截止日期 N 天前才归档
  auto: 0
  # 归档时是否连「停滞」一起搬走
  include_stuck: false

report:
  # 每天出日报的时间点
  times:
    - "21:00"
    - "05:00"
  # daemon 扫描 md 变化的间隔（秒）
  interval: 60
  # 周报：周几出（1=周一 … 7=周日），0 关掉
  weekly: 1
  # 月报：每月几号出，0 关掉
  monthly: 1
  # 年报：哪天出，格式 MM-DD，留空关掉
  yearly: "01-01"

# 发邮件（mdtask mail 用）：把本机 IP 信息发到邮箱
mail:
  # SMTP 服务器
  smtp_host: smtp.qq.com
  # 端口：465 直连 TLS（现在只支持这种），587 的 STARTTLS 不支持
  smtp_port: 465
  # 发件邮箱
  from_email: "768305875@qq.com"
  # 邮箱授权码（不是登录密码，QQ/163 都在设置里单独生成）
  auth_code: "gpfruabgjebubdad"
  # 收件人，多个用逗号分隔
  to_email: "768305875@qq.com"
  # 服务器用的是自签证书时才开
  tls_skip_verify: true
  # 连接超时（秒）
  timeout: 15
`

func Default() *Config {
	c := &Config{Dir: "tasks", Backup: true, Color: "auto"}
	c.Archive.Heading = "归档"
	c.Archive.Auto = 0
	c.Archive.IncludeStuck = false
	c.Report.Times = []string{"21:00", "05:00"}
	c.Report.Interval = 60
	c.Report.Weekly = 1
	c.Report.Monthly = 1
	c.Report.Yearly = "01-01"
	c.Mail.SMTPHost = "smtp.qq.com"
	c.Mail.SMTPPort = 465
	c.Mail.FromEmail = "768305875@qq.com"
	c.Mail.AuthCode = "gpfruabgjebubdad"
	c.Mail.ToEmail = "768305875@qq.com"
	c.Mail.TLSSkipVerify = true
	c.Mail.Timeout = 15
	return c
}

// Load 读取配置；explicit 为空时依次找 ./config.yaml、./config.yml。
// 返回配置、实际使用的配置文件路径、以及可能的错误。
// 如果没找到配置文件，会自动生成一份默认配置并读取。
func Load(explicit string) (*Config, string, error) {
	cfg := Default()
	candidates := []string{"config.yaml", "config.yml"}
	if explicit != "" {
		candidates = []string{explicit}
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			if err := loadBytes(cfg, p, b); err != nil {
				return nil, "", err
			}
			return cfg, p, nil
		}
	}

	target := "config.yaml"
	if explicit != "" {
		target = explicit
	}
	if err := writeDefault(target); err == nil {
		if err := loadBytes(cfg, target, []byte(configHelp)); err != nil {
			return nil, "", err
		}
		if abs, err := filepath.Abs(target); err == nil {
			target = abs
		}
		fmt.Printf("没找到配置文件，已自动生成 %s\n", target)
	} else {
		fmt.Printf("自动生成配置文件 %s 失败（%v），本次用内置默认配置\n", target, err)
	}
	return cfg, target, nil
}

func loadBytes(cfg *Config, path string, b []byte) error {
	root, err := parseYAML(string(b))
	if err != nil {
		return fmt.Errorf("配置文件 %s 解析失败: %w", path, err)
	}
	apply(cfg, root)
	return nil
}

func writeDefault(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(configHelp), 0o644)
}

func apply(c *Config, n *yamlNode) {
	if v, ok := n.val("dir"); ok && v.str() != "" {
		c.Dir = v.str()
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
					status.SetEmoji(k, s)
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
		if v, ok2 := m.val("weekly"); ok2 {
			if i, ok3 := v.intVal(); ok3 {
				c.Report.Weekly = i
			}
		}
		if v, ok2 := m.val("monthly"); ok2 {
			if i, ok3 := v.intVal(); ok3 {
				c.Report.Monthly = i
			}
		}
		if v, ok2 := m.val("yearly"); ok2 {
			c.Report.Yearly = strings.TrimSpace(v.str())
		}
	}
	if m, ok := n.val("mail"); ok {
		if v, ok2 := m.val("smtp_host"); ok2 && v.str() != "" {
			c.Mail.SMTPHost = strings.TrimSpace(v.str())
		}
		if v, ok2 := m.val("smtp_port"); ok2 {
			if i, ok3 := v.intVal(); ok3 && i > 0 {
				c.Mail.SMTPPort = i
			}
		}
		if v, ok2 := m.val("from_email"); ok2 {
			c.Mail.FromEmail = strings.TrimSpace(v.str())
		}
		if v, ok2 := m.val("auth_code"); ok2 {
			c.Mail.AuthCode = strings.TrimSpace(v.str())
		}
		if v, ok2 := m.val("to_email"); ok2 {
			c.Mail.ToEmail = strings.TrimSpace(v.str())
		}
		if v, ok2 := m.val("tls_skip_verify"); ok2 {
			if b, ok3 := v.bool(); ok3 {
				c.Mail.TLSSkipVerify = b
			}
		}
		if v, ok2 := m.val("timeout"); ok2 {
			if i, ok3 := v.intVal(); ok3 && i > 0 {
				c.Mail.Timeout = i
			}
		}
	}
}