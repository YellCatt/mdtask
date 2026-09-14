package config

import (
	"strconv"
	"strings"
)

// A small YAML subset parser: comments, indented nesting, key: value,
// list items and [a, b] inline lists. No anchors, multi-line strings or
// fancy indentation - the config files don't need them.
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

// parseYAML 把配置文本拆成「去注释、计缩进」后的行序列，再递归解析成节点树。
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

// parseBlock 解析同一缩进层级下的一个 map 或 list 块，返回解析出的节点与消费到的行号。
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

// scalarNode 把一段文本转成 scalar 或 [a,b] 内联列表节点。
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

// isSeqItem 判断一行是否为列表项（以 "-" 或 "- " 开头）。
func isSeqItem(s string) bool {
	return s == "-" || strings.HasPrefix(s, "- ")
}

// splitKV splits on the first "key: " outside quotes.
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

// stripComment 去掉行内注释；只在引号外、且 # 前面是空白时才当注释处理，避免误删内容。
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

// unquote 去掉字符串首尾的单/双引号（若成对）。
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ---------- Accessors ----------

// val 取 map 节点下某个 key 对应的子节点。
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

// intVal 把 scalar 解析成整数，失败返回 (0,false)。
func (n *yamlNode) intVal() (int, bool) {
	if n == nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(n.scalar))
	if err != nil {
		return 0, false
	}
	return v, true
}

// bool 把 scalar 解析成布尔（兼容 true/yes/on/1 与 false/no/off/0），失败返回 (false,false)。
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

// strings 把节点展开成字符串切片：列表逐项取值，单个 scalar 返回单元素切片。
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