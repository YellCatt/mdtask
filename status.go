package main

import (
	"strings"
	"time"
)

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

// ---------- 显示 ----------

func statusLabel(v string) string {
	if d := statusDefOf(v); d != nil {
		return d.Key + " " + d.Label // md 里存 emoji，终端补上中文名
	}
	s := strings.TrimSpace(v)
	if s == "" || strings.ToLower(s) == "todo" { // 空状态（含旧写法 todo）= 待办
		return "⬜ 待办"
	}
	return "• " + s
}

func statusColor(v string) string {
	if d := statusDefOf(v); d != nil {
		switch d.Label {
		case "完成":
			return cGreen
		case "进行中":
			return cBlue
		case "停滞":
			return cYel
		case "取消":
			return cDim
		}
	}
	return "" // 待办 / 未知状态不染色
}

func prioLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return "高"
	case "mid":
		return "中"
	case "low":
		return "低"
	}
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func prioColor(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return cRed
	case "mid":
		return cYel
	case "low":
		return cGray
	}
	return cGray
}

// dueColor 未完成且已过期 → 红色
func dueColor(t Task, v string) string {
	if v == "" || isClosedStatus(t.Status) {
		return cGray
	}
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(v)); err == nil {
		if d.Before(time.Now().Truncate(24 * time.Hour)) {
			return cRed
		}
	}
	return cGray
}

func dueText(t Task, v string) string {
	if v == "" {
		return paint(cGray, "（空）")
	}
	return paint(dueColor(t, v), v)
}

// cmdNameOfStatus 给命令取个名字，用于报错时的用法提示
func cmdNameOfStatus(key string) string {
	for i := range statusDefs {
		if statusDefs[i].Key == key {
			return strings.ToLower(statusDefs[i].Alias[0])
		}
	}
	return "todo"
}
