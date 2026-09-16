// Package status 定义任务的四种状态（完成/进行中/停滞/取消）及其别名、
// 排序权重，并提供「任意写法 → 规范状态」的识别函数。
package status

import (
	"strings"
)

// 四种状态的规范值 —— 写进 md 的就是这些 emoji
const (
	Done   = "✅"
	Doing  = "⏸\ufe0f"
	Hold   = "❌"
	Cancel = "\U0001f534"
)

// StatusDef 描述一种任务状态：规范 emoji、中文标签、是否已结束、排序权重、别名表。
type StatusDef struct {
	Key   string
	Label string
	Closed bool
	Rank  int
	Alias []string
}

// defs 内置四种状态定义；别名同时支持中英文，便于不同写法归一到同一状态。
var defs = []StatusDef{
	{
		Key: Done, Label: "完成", Closed: true, Rank: 3,
		Alias: []string{"done", "finished", "finish", "complete", "完成", "已完成", "完"},
	},
	{
		Key: Doing, Label: "进行中", Closed: false, Rank: 1,
		Alias: []string{"doing", "wip", "进行中", "进行", "在做"},
	},
	{
		Key: Hold, Label: "停滞", Closed: false, Rank: 2,
		Alias: []string{"hold", "stuck", "blocked", "pause", "停滞", "暂停", "搁置", "卡住"},
	},
	{
		Key: Cancel, Label: "取消", Closed: true, Rank: 3,
		Alias: []string{"cancel", "canceled", "cancelled", "drop", "abort", "取消", "放弃", "已取消"},
	},
}

// statusIndex 把英文状态名映射到 defs 下标，供 SetEmoji、IsHold 等精确定位。
var statusIndex = map[string]int{"done": 0, "doing": 1, "hold": 2, "cancel": 3}

// stripVS 去掉 emoji 的变体选择符（\ufe0e/\ufe0f），让比较不受平台渲染差异影响。
func stripVS(s string) string {
	return strings.NewReplacer("\ufe0e", "", "\ufe0f", "").Replace(s)
}

// matches 判断给定字符串（状态码/中文标签/别名）是否匹配该状态定义。
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

// DefOf 根据任意写法（emoji/中文/英文别名）返回对应的状态定义，找不到返回 nil。
func DefOf(v string) *StatusDef {
	for i := range defs {
		if defs[i].matches(v) {
			return &defs[i]
		}
	}
	return nil
}

// IsClosed 判断该状态是否「已结束」（完成或取消）。
func IsClosed(v string) bool {
	if d := DefOf(v); d != nil {
		return d.Closed
	}
	return false
}

// IsHold 判断是否为「停滞」状态；按定义下标比较，不受自定义 emoji 影响。
func IsHold(v string) bool {
	return DefOf(v) == &defs[statusIndex["hold"]]
}

// IsDoing 判断是否为「进行中」状态；按定义下标比较，不受自定义 emoji 影响。
func IsDoing(v string) bool {
	return DefOf(v) == &defs[statusIndex["doing"]]
}

// RankOf 返回状态排序权重，未结束且更紧急的状态权重大；空状态当作普通待办。
func RankOf(v string) int {
	if strings.TrimSpace(v) == "" {
		return 0
	}
	if d := DefOf(v); d != nil {
		return d.Rank
	}
	return 1
}

// Canonical 返回状态的规范 emoji；无法识别时原样返回，便于无损回写。
func Canonical(v string) string {
	if d := DefOf(v); d != nil {
		return d.Key
	}
	return strings.TrimSpace(v)
}

// SetEmoji 用配置里的 emoji 覆盖某个状态的规范值，并把旧值追加到别名，
// 避免之前用旧 emoji 写的任务再读取时无法匹配。
func SetEmoji(name, emoji string) {
	i, ok := statusIndex[name]
	if !ok {
		return
	}
	old := defs[i].Key
	if old == emoji {
		return
	}
	defs[i].Key = emoji
	defs[i].Alias = append(defs[i].Alias, old)
}

// Label 返回用于展示的「emoji 中文标签」；空/未知状态返回通用占位符。
func Label(v string) string {
	if d := DefOf(v); d != nil {
		return d.Key + " " + d.Label
	}
	s := strings.TrimSpace(v)
	if s == "" || strings.ToLower(s) == "todo" {
		return "⬜ 待办"
	}
	return "• " + s
}

// CmdNameOf 根据状态 emoji 返回其英文命令名（取第一个别名），用于 CLI 子命令映射。
func CmdNameOf(key string) string {
	for i := range defs {
		if defs[i].Key == key {
			return strings.ToLower(defs[i].Alias[0])
		}
	}
	return "todo"
}