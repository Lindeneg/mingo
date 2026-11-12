package kit

import (
	"testing"
)

func reader(files map[string]string) func(string) ([]byte, bool) {
	return func(p string) ([]byte, bool) {
		text, ok := files[p]
		return []byte(text), ok
	}
}

func actions(steps []Step) map[string]Action {
	out := map[string]Action{}
	for _, s := range steps {
		out[s.File.Path] = s.Action
	}
	return out
}

func TestPlanAddsReplacesKeepsAndShowsByWhatTheProjectAndMingoChanged(t *testing.T) {
	rendered := []File{
		{Path: "new.md", Module: "m", Text: "new\n"},
		{Path: "same.md", Module: "m", Text: "same\n"},
		{Path: "ours.md", Module: "m", Text: "v2\n"},
		{Path: "edited.md", Module: "m", Text: "v2\n"},
		{Path: "kept.md", Module: "m", Text: "v1\n"},
		{Path: "theirs.md", Module: "m", Text: "v2\n"},
	}
	repo := reader(map[string]string{
		"same.md":   "same\n",
		"ours.md":   "v1\n",
		"edited.md": "v1 and the project's line\n",
		"kept.md":   "v1 and the project's line\n",
		"theirs.md": "the project's own\n",
	})
	lock := &Lock{Files: []Locked{
		{Path: "ours.md", Sum: Sum([]byte("v1\n"))},
		{Path: "edited.md", Sum: Sum([]byte("v1\n"))},
		{Path: "kept.md", Sum: Sum([]byte("v1\n"))},
	}}
	got := actions(Plan(rendered, repo, lock))
	want := map[string]Action{"new.md": Add, "same.md": Same, "ours.md": Update, "edited.md": Edited, "kept.md": Kept, "theirs.md": Edited}
	for p, a := range want {
		if got[p] != a {
			t.Fatalf("%s: %s, want %s", p, got[p], a)
		}
	}
}

func TestNextLockRecordsWhatWasWrittenAndAFileFoundAsRenderedOnly(t *testing.T) {
	steps := []Step{
		{File: File{Path: "a", Module: "m", Text: "a"}, Action: Add},
		{File: File{Path: "b", Module: "m", Text: "b"}, Action: Same},
		{File: File{Path: "c", Module: "m", Text: "c"}, Action: Edited},
		{File: File{Path: "d", Module: "m", Text: "d"}, Action: Same},
	}
	lock := &Lock{Files: []Locked{{Path: "d", Module: "m", Mingo: "old", Sum: Sum([]byte("d"))}}}
	next, changed := NextLock(steps, lock, "v")
	if !changed || len(next.Files) != 3 {
		t.Fatal(next.Files)
	}
	if d, _ := next.Get("d"); d.Mingo != "old" {
		t.Fatal(d)
	}
	if _, ok := next.Get("c"); ok {
		t.Fatal("an edited file was locked")
	}
	again, changed := NextLock([]Step{{File: File{Path: "d", Text: "d"}, Action: Same}}, lock, "v")
	if changed || len(again.Files) != 1 {
		t.Fatal(again.Files)
	}
}

func TestTheLockReadsBackWhatItWrites(t *testing.T) {
	l := &Lock{Files: []Locked{{Path: "z.md", Module: "b", Mingo: "v1", Sum: "s1"}, {Path: "a.md", Module: "a", Mingo: "v2", Sum: "s2"}}}
	back, err := ParseLock(l.Bytes())
	if err != nil || len(back.Files) != 2 || back.Files[0].Path != "a.md" || back.Files[1] != l.Files[0] {
		t.Fatal(back, err, string(l.Bytes()))
	}
	empty, err := ParseLock(nil)
	if err != nil || len(empty.Files) != 0 {
		t.Fatal(empty, err)
	}
}

func TestTheLockReadsBackAPathWithCharactersGoQuotesOtherwise(t *testing.T) {
	odd := Locked{Path: "docs/\a\v\x7f é \"q\"\\.md", Module: "m", Mingo: "v", Sum: "s"}
	back, err := ParseLock((&Lock{Files: []Locked{odd}}).Bytes())
	if err != nil || len(back.Files) != 1 || back.Files[0] != odd {
		t.Fatal(back, err)
	}
}
