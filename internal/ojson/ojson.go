// Package ojson reads and writes JSON with the order of object keys kept.
package ojson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Object struct {
	keys []string
	vals map[string]any
}

func NewObject() *Object {
	return &Object{vals: map[string]any{}}
}

func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *Object) Set(key string, v any) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// sets key right after another key, or at the end when that one is missing
func (o *Object) InsertAfter(after, key string, v any) {
	if _, ok := o.vals[key]; ok {
		o.vals[key] = v
		return
	}
	o.vals[key] = v
	for i, k := range o.keys {
		if k == after {
			o.keys = append(o.keys[:i+1], append([]string{key}, o.keys[i+1:]...)...)
			return
		}
	}
	o.keys = append(o.keys, key)
}

func (o *Object) Keys() []string {
	return o.keys
}

func (o *Object) Vals(key string) (*Object, bool) {
	v, ok := o.vals[key].(*Object)
	return v, ok
}

func (o *Object) Child(key string) *Object {
	if v, ok := o.vals[key].(*Object); ok {
		return v
	}
	child := NewObject()
	o.Set(key, child)
	return child
}

func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := value(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the JSON value")
	}
	return v, nil
}

func value(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := value(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k.(string), v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			list := []any{}
			for dec.More() {
				v, err := value(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := dec.Token()
			return list, err
		}
		return nil, errors.New("unexpected delimiter")
	default:
		return t, nil
	}
}

func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := write(&b, v, 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func write(b *bytes.Buffer, v any, depth int) error {
	indent := strings.Repeat("  ", depth+1)
	switch t := v.(type) {
	case *Object:
		if len(t.keys) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteString("{\n")
		for i, k := range t.keys {
			b.WriteString(indent)
			writeString(b, k)
			b.WriteString(": ")
			if err := write(b, t.vals[k], depth+1); err != nil {
				return err
			}
			if i < len(t.keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteString("[\n")
		for i, item := range t {
			b.WriteString(indent)
			if err := write(b, item, depth+1); err != nil {
				return err
			}
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat("  ", depth) + "]")
	case string:
		writeString(b, t)
	case json.Number:
		b.WriteString(t.String())
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case nil:
		b.WriteString("null")
	case int:
		data, _ := json.Marshal(t)
		b.Write(data)
	default:
		return errors.New("no JSON form for this value")
	}
	return nil
}

func writeString(b *bytes.Buffer, s string) {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	b.Write(bytes.TrimSuffix(tmp.Bytes(), []byte("\n")))
}
