package team

import (
	"strings"
	"testing"
)

func fuzzLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || l == "a" || l == "z" || strings.ContainsAny(l[:1], "<|=>") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func FuzzKeepBothSides(f *testing.F) {
	f.Add("- m1\n- m2", "", "- f1")
	f.Add("- m1\n- e1", "- e1", "- e1x")
	f.Add("- x\n- m", "", "- x")
	f.Add("- e1x\n- k", "- e1\n- k", "- k\n- e")
	f.Fuzz(func(t *testing.T, ours, base, theirs string) {
		o, b, th := fuzzLines(ours), fuzzLines(base), fuzzLines(theirs)
		out := KeepBothSides(conflict(o, b, th))
		if !out.Merged() {
			return
		}
		lines := strings.Split(strings.TrimSuffix(out.Text, "\n"), "\n")
		if lines[0] != "a" || lines[len(lines)-1] != "z" {
			t.Fatalf("the lines around the conflict moved: %q", out.Text)
		}
		if len(b) > 0 {
			return
		}
		for _, l := range append(append([]string(nil), o...), th...) {
			if !contains(lines, l) {
				t.Fatalf("%q was added by a side and is gone: %q", l, out.Text)
			}
		}
	})
}

func FuzzMergeJSONKeys(f *testing.F) {
	f.Add(`{"a":"1"}`, `{"a":"1","m":"2"}`, `{"a":"1","f":"3"}`)
	f.Add(`{"a":"1"}`, `{"a":"2"}`, `{"a":"1","f":"3"}`)
	f.Add(`{"a":{"b":1}}`, `{"a":{"b":2}}`, `{"a":{"b":3}}`)
	f.Fuzz(func(t *testing.T, base, ours, theirs string) {
		out := MergeJSONKeys(&base, ours, theirs)
		if !out.Merged() {
			return
		}
		merged := parseObject(out.Text)
		if merged == nil {
			t.Fatalf("the merge is no JSON object: %q", out.Text)
		}
		b := parseObject(base)
		for _, side := range []string{ours, theirs} {
			for _, k := range parseObject(side).Keys() {
				if _, was := b.Get(k); !was && !has(merged, k) {
					t.Fatalf("the key %q a side added is gone: %q", k, out.Text)
				}
			}
		}
	})
}
