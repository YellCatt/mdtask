package config

import (
	"strconv"
	"strings"
)

// 一个够用的 YAML 子集解析器：支持注释、缩进嵌套、key: value�? 列表、[a, b] 内联列表�?// 不支持锚点、多行字符串、复杂缩进，配置文件用不到那些�?
const (
	kindScalar = 0
	kindMap    = 1
	kindSeq    = 2
)

type yamlNode struct {
	kind   int
	scalar string
	items  map[string]*yamlNode
	list   []*yamlNode
}

type yamlLine struct {
	indent int
	text   string
}

func parseYAML(src string) (*yamlNode, error) {
	var ls []yamlLine
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(stripComment(raw)) == "" {
			continue
		}
		indent := 0
		for _, c := range raw {
			if c == ' ' {
				indent++
				continue
			}
			if c == '\t' {
				indent += 2
				continue
			}
			break
		}
		ls = append(ls, yamlLine{indent, strings.TrimSpace(stripComment(raw))})
	}
	if len(ls) == 0 {
		return &yamlNode{kind: kindMap, items: map[string]*yamlNode{}}, nil
	}
	n, _ := parseBlock(ls, 0, ls[0].indent)
	return n, nil
}

func parseBlock(ls []yamlLine, i, indent int) (*yamlNode, int) {
	if i >= len(ls) {
		return &yamlNode{kind: kindMap, items: map[string]*yamlNode{}}, i
	}
	if isSeqItem(ls[i].text) {
		n := &yamlNode{kind: kindSeq}
		for i < len(ls) && ls[i].indent == indent && isSeqItem(ls[i].text) {
			item := strings.TrimSpace(strings.TrimPrefix(ls[i].text, "-"))
			i++
			if item == "" {
				continue
			}
			n.list = append(n.list, scalarNode(item))
		}
		return n, i
	}
	n := &yamlNode{kind: kindMap, items: map[string]*yamlNode{}}
	for i < len(ls) && ls[i].indent == indent && !isSeqItem(ls[i].text) {
		key, val, hasVal := splitKV(ls[i].text)
		i++
		if key == "" {
			continue
		}
		switch {
		case hasVal:
			n.items[key] = scalarNode(val)
		case i < len(ls) && ls[i].indent > indent:
			child, ni := parseBlock(ls, i, ls[i].indent)
			n.items[key] = child
			i = ni
		default:
			n.items[key] = scalarNode("")
		}
	}
	return n, i
}

func scalarNode(s string) *yamlNode {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		n := &yamlNode{kind: kindSeq}
		for _, p := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"), ",") {
			p = unquote(strings.TrimSpace(p))
			if p != "" {
				n.list = append(n.list, &yamlNode{kind: kindScalar, scalar: p})
			}
		}
		return n
	}
	return &yamlNode{kind: kindScalar, scalar: unquote(s)}
}

func isSeqItem(s string) bool {
	return s == "-" || strings.HasPrefix(s, "- ")
}

// splitKV 用第一个不在引号内�?"key: " 切分
func splitKV(s string) (string, string, bool) {
	var inQ byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQ != 0 {
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		if c == ':' && (i+1 == len(s) || s[i+1] == ' ') {
			val := ""
			hasVal := i+1 < len(s)
			if hasVal {
				val = strings.TrimSpace(s[i+1:])
			}
			return unquote(strings.TrimSpace(s[:i])), val, strings.TrimSpace(val) != ""
		}
	}
	return unquote(strings.TrimSpace(s)), "", false
}

func stripComment(line string) string {
	var inQ byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQ != 0 {
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		if c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ---------- 取�?----------

func (n *yamlNode) val(key string) (*yamlNode, bool) {
	if n == nil || n.kind != kindMap {
		return nil, false
	}
	v, ok := n.items[key]
	return v, ok
}

func (n *yamlNode) str() string {
	if n == nil {
		return ""
	}
	return n.scalar
}

func (n *yamlNode) intVal() (int, bool) {
	if n == nil {
		return 0, false
	}
	return strconv.Atoi(strings.TrimSpace(n.scalar))
}

func (n *yamlNode) bool() (bool, bool) {
	if n == nil {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(n.scalar)) {
	case "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0":
		return false, true
	}
	return false, false
}

func (n *yamlNode) strings() []string {
	if n == nil {
		return nil
	}
	if n.kind == kindSeq {
		out := make([]string, 0, len(n.list))
		for _, it := range n.list {
			out = append(out, it.str())
		}
		return out
	}
	if n.scalar != "" {
		return []string{n.scalar}
	}
	return nil
}

