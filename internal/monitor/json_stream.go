package monitor

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

const MaxScan = 8 << 20

// ScanJSON skips long strings (notably image data) without materializing them.
// encoding/json.Decoder.Token allocates the entire string before returning it,
// so it cannot enforce the image/memory boundary. This bounded framing scanner
// delegates scalar validation, unescaping and final encoding to encoding/json.
// A scan/output/depth limit yields a valid partial object, never an invented full body.
func ScanJSON(r io.Reader) (json.RawMessage, bool) {
	limited := &io.LimitedReader{R: r, N: MaxScan}
	p := &jsonScan{r: bufio.NewReaderSize(limited, 4096), budget: MaxBody / 2}
	v := p.value("", 0)
	if _, err := p.next(); err != io.EOF {
		p.partial = true
	}
	if limited.N == 0 {
		p.partial = true
	}
	if v == nil {
		v = map[string]any{"omitted": "unavailable JSON"}
		p.partial = true
	}
	data, _ := json.Marshal(v)
	clean, filtered := SanitizeJSON(data)
	return clean, p.partial || filtered
}

type jsonScan struct {
	r               *bufio.Reader
	budget          int
	partial, failed bool
}

func (p *jsonScan) next() (byte, error) {
	for {
		b, e := p.r.ReadByte()
		if e != nil {
			return 0, e
		}
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			return b, nil
		}
	}
}
func (p *jsonScan) string(skip bool) string {
	buf := make([]byte, 0, 128)
	if !skip {
		buf = append(buf, '"')
	}
	escaped, overflow := false, false
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			p.failed, p.partial = true, true
			return "[incomplete]"
		}
		if !skip && !overflow {
			if len(buf) >= 16384 || len(buf) >= p.budget {
				overflow = true
				buf = nil
				p.partial = true
			} else {
				buf = append(buf, b)
			}
		}
		if b == '"' && !escaped {
			break
		}
		if b == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
	}
	if skip || overflow {
		p.partial = true
		return "[withheld or string limit]"
	}
	p.budget -= len(buf)
	var s string
	if json.Unmarshal(buf, &s) != nil {
		p.failed, p.partial = true, true
		return "[invalid string]"
	}
	return s
}
func (p *jsonScan) value(key string, depth int) any {
	if p.failed {
		return nil
	}
	if depth > 24 || p.budget <= 0 {
		p.failed, p.partial = true, true
		return "[structure limit]"
	}
	b, err := p.next()
	if err != nil {
		p.failed, p.partial = true, true
		return "[incomplete]"
	}
	p.budget -= 32
	switch b {
	case '"':
		return p.string(privateKey(key))
	case '{':
		out := map[string]any{}
		for !p.failed {
			b, err = p.next()
			if err != nil {
				break
			}
			if b == '}' {
				if len(out) > 0 {
					p.partial = true
				}
				return out
			}
			if b != '"' {
				break
			}
			k := p.string(false)
			b, err = p.next()
			if err != nil || b != ':' {
				break
			}
			out[k] = p.value(k, depth+1)
			b, err = p.next()
			if err != nil {
				break
			}
			if b == '}' {
				return out
			}
			if b != ',' {
				break
			}
		}
		p.failed, p.partial = true, true
		return out
	case '[':
		out := []any{}
		for !p.failed {
			b, err = p.next()
			if err != nil {
				break
			}
			if b == ']' {
				if len(out) > 0 {
					p.partial = true
				}
				return out
			}
			_ = p.r.UnreadByte()
			if len(out) >= 1024 {
				break
			}
			out = append(out, p.value(key, depth+1))
			b, err = p.next()
			if err != nil {
				break
			}
			if b == ']' {
				return out
			}
			if b != ',' {
				break
			}
		}
		p.failed, p.partial = true, true
		return out
	default:
		buf := []byte{b}
		for len(buf) <= 128 {
			b, err = p.r.ReadByte()
			if err != nil {
				break
			}
			if strings.ContainsRune(",}] \r\n\t", rune(b)) {
				_ = p.r.UnreadByte()
				break
			}
			buf = append(buf, b)
		}
		var out any
		if json.Unmarshal(buf, &out) != nil {
			p.failed, p.partial = true, true
			return "[invalid value]"
		}
		return out
	}
}
