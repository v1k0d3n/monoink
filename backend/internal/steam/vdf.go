package steam

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Node is a parsed text-VDF (KeyValues) object. Values are either string
// or Node. Steam's key casing is inconsistent, so lookups ignore case.
type Node map[string]any

// Get follows a path of keys, case-insensitively.
func (n Node) Get(path ...string) (any, bool) {
	var cur any = n
	for _, k := range path {
		m, ok := cur.(Node)
		if !ok {
			return nil, false
		}
		v, ok := m[k]
		if !ok {
			found := false
			for mk, mv := range m {
				if strings.EqualFold(mk, k) {
					v, found = mv, true
					break
				}
			}
			if !found {
				return nil, false
			}
		}
		cur = v
	}
	return cur, true
}

// Str returns the string at path, or "".
func (n Node) Str(path ...string) string {
	v, _ := n.Get(path...)
	s, _ := v.(string)
	return s
}

// Child returns the object at path, or nil.
func (n Node) Child(path ...string) Node {
	v, _ := n.Get(path...)
	c, _ := v.(Node)
	return c
}

// ParseVDF parses Valve's text KeyValues format.
func ParseVDF(r io.Reader) (Node, error) {
	p := &vdfParser{r: bufio.NewReaderSize(r, 64<<10)}
	return p.object(true)
}

type vdfParser struct{ r *bufio.Reader }

func (p *vdfParser) token() (string, bool, error) {
	for {
		c, err := p.r.ReadByte()
		if err != nil {
			return "", false, err
		}
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
		case c == '/':
			if n, _ := p.r.Peek(1); len(n) == 1 && n[0] == '/' {
				p.r.ReadString('\n')
			}
		case c == '{' || c == '}':
			return string(c), false, nil
		case c == '"':
			var sb strings.Builder
			for {
				c, err := p.r.ReadByte()
				if err != nil {
					return "", false, err
				}
				if c == '\\' {
					e, err := p.r.ReadByte()
					if err != nil {
						return "", false, err
					}
					switch e {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					default:
						sb.WriteByte(e)
					}
					continue
				}
				if c == '"' {
					return sb.String(), true, nil
				}
				sb.WriteByte(c)
			}
		default:
			// Unquoted token (rare, e.g. conditionals); read to whitespace.
			var sb strings.Builder
			sb.WriteByte(c)
			for {
				n, err := p.r.Peek(1)
				if err != nil || strings.ContainsRune(" \t\r\n{}\"", rune(n[0])) {
					return sb.String(), true, nil
				}
				b, _ := p.r.ReadByte()
				sb.WriteByte(b)
			}
		}
	}
}

func (p *vdfParser) object(root bool) (Node, error) {
	n := Node{}
	for {
		key, isStr, err := p.token()
		if errors.Is(err, io.EOF) && root {
			return n, nil
		}
		if err != nil {
			return nil, err
		}
		if !isStr {
			if key == "}" && !root {
				return n, nil
			}
			return nil, errors.New("vdf: unexpected " + key)
		}
		val, isStr, err := p.token()
		if err != nil {
			return nil, err
		}
		if isStr {
			n[key] = val
			continue
		}
		if val != "{" {
			return nil, errors.New("vdf: expected { after " + key)
		}
		child, err := p.object(false)
		if err != nil {
			return nil, err
		}
		n[key] = child
	}
}
