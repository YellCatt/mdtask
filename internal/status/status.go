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

type StatusDef struct {
	Key   string
	Label string
	Closed bool
	Rank  int
	Alias []string
}

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

var statusIndex = map[string]int{"done": 0, "doing": 1, "hold": 2, "cancel": 3}

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

func DefOf(v string) *StatusDef {
	for i := range defs {
		if defs[i].matches(v) {
			return &defs[i]
		}
	}
	return nil
}

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

func RankOf(v string) int {
	if strings.TrimSpace(v) == "" {
		return 0
	}
	if d := DefOf(v); d != nil {
		return d.Rank
	}
	return 1
}

func Canonical(v string) string {
	if d := DefOf(v); d != nil {
		return d.Key
	}
	return strings.TrimSpace(v)
}

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

func CmdNameOf(key string) string {
	for i := range defs {
		if defs[i].Key == key {
			return strings.ToLower(defs[i].Alias[0])
		}
	}
	return "todo"
}