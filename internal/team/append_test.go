package team

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/ojson"
)

var (
	open  = strings.Repeat("<", MarkerSize) + " ours"
	base  = strings.Repeat("|", MarkerSize) + " base"
	split = strings.Repeat("=", MarkerSize)
	close = strings.Repeat(">", MarkerSize) + " theirs"
)

func text(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func join(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func conflict(ours, was, theirs []string) string {
	return text(join([]string{"a", open}, ours, []string{base}, was, []string{split}, theirs, []string{close, "z"})...)
}

func merged(took []Taken, lines ...string) Outcome {
	return Outcome{Text: text(join([]string{"a"}, lines, []string{"z"})...), Took: took}
}

func same(t *testing.T, got, want Outcome) {
	t.Helper()
	if want.Took == nil && want.Merged() {
		want.Took = []Taken{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func took(t ...Taken) []Taken {
	return t
}

var twelve = func() []string {
	var out []string
	for i := 1; i <= 12; i++ {
		out = append(out, fmt.Sprintf("- m%d", i))
	}
	return out
}()

func TestKeepBothSidesReturnsAFileWithoutAConflictAsItIs(t *testing.T) {
	same(t, KeepBothSides(text("a", "b")), Outcome{Text: text("a", "b")})
}

func TestKeepBothSidesKeepsTheFirstSidesAddedLinesThenTheSeconds(t *testing.T) {
	marked := text("a", open, "ours 1", "ours 2", base, split, "theirs 1", close, "z")
	same(t, KeepBothSides(marked), Outcome{Text: text("a", "ours 1", "ours 2", "theirs 1", "z"), Took: took(Additions)})
}

func TestKeepBothSidesResolvesEveryConflictOfTheFile(t *testing.T) {
	marked := text(open, "o1", base, split, "t1", close, "mid", open, "o2", base, split, "t2", close)
	same(t, KeepBothSides(marked), Outcome{Text: text("o1", "t1", "mid", "o2", "t2"), Took: took(Additions)})
}

func TestKeepBothSidesRefusesAConflictOverALineBothSidesChanged(t *testing.T) {
	marked := text("a", open, "ours", base, "was here", split, "theirs", close)
	same(t, KeepBothSides(marked), edited("both sides changed or removed a line that was already there"))
}

func TestKeepBothSidesRefusesMarkersThatDoNotPairUp(t *testing.T) {
	if KeepBothSides(text("a", open, "ours", base, split, "theirs")).Merged() {
		t.Fatal("merged")
	}
}

func TestKeepBothSidesTakesGitsOwnSevenCharacterMarkersForText(t *testing.T) {
	marked := text("<<<<<<< ours", "=======", ">>>>>>> theirs")
	same(t, KeepBothSides(marked), Outcome{Text: marked})
}

func TestKeepBothSidesTakesTheLinesBothSidesAddedAlikeAtTheEndsOnce(t *testing.T) {
	marked := text(open, "same", "o", "end", base, split, "same", "t", "end", close)
	same(t, KeepBothSides(marked), Outcome{Text: text("same", "o", "t", "end"), Took: took(Additions)})
}

func TestKeepBothSidesTakesTheOneEntryALaterCommitRewroteBehindWhatMasterAdded(t *testing.T) {
	same(t, KeepBothSides(conflict(join(twelve, []string{"- e1"}), []string{"- e1"}, []string{"- e1, rewritten"})),
		merged(took(Additions, SecondRewrite), join(twelve, []string{"- e1, rewritten"})...))
}

func TestKeepBothSidesTakesBothEntriesALaterCommitRewrote(t *testing.T) {
	same(t, KeepBothSides(conflict(join(twelve, []string{"- e1", "- e2"}), []string{"- e1", "- e2"}, []string{"- e1x", "- e2x"})),
		merged(took(Additions, SecondRewrite), join(twelve, []string{"- e1x", "- e2x"})...))
}

func TestKeepBothSidesLeavesOutAnEntryALaterCommitRemoved(t *testing.T) {
	same(t, KeepBothSides(conflict(join(twelve, []string{"- e1", "- e2"}), []string{"- e1", "- e2"}, []string{"- e2"})),
		merged(took(Additions, SecondRewrite), join(twelve, []string{"- e2"})...))
	same(t, KeepBothSides(conflict([]string{"- m", "- e1", "- e2"}, []string{"- e1", "- e2"}, nil)),
		merged(took(Additions, SecondRewrite), "- m"))
}

func TestKeepBothSidesTakesARewriteAndAThirdAppendedBehindItMastersLinesFirst(t *testing.T) {
	same(t, KeepBothSides(conflict([]string{"- m", "- e1"}, []string{"- e1"}, []string{"- e1x", "- e3"})),
		merged(took(Additions, SecondRewrite), "- m", "- e1x", "- e3"))
}

func TestKeepBothSidesTakesAnEntryMasterRewroteWhileTheBranchAppended(t *testing.T) {
	same(t, KeepBothSides(conflict([]string{"- m1x"}, []string{"- m1"}, []string{"- m1", "- e"})),
		merged(took(Additions, FirstRewrite), "- m1x", "- e"))
	same(t, KeepBothSides(conflict([]string{"- m1x"}, []string{"- m1"}, []string{"- e", "- m1"})),
		merged(took(Additions, FirstRewrite), "- e", "- m1x"))
}

func TestKeepBothSidesTakesOneLineRewrittenOrLinesRemovedWhereMasterAddedBehindThem(t *testing.T) {
	same(t, KeepBothSides(conflict([]string{"- e1", "- m"}, []string{"- e1"}, []string{"- e1x"})),
		merged(took(Additions, SecondRewrite), "- e1x", "- m"))
	same(t, KeepBothSides(conflict([]string{"- e1", "- e2", "- m"}, []string{"- e1", "- e2"}, nil)),
		merged(took(Additions, SecondRewrite), "- m"))
}

func TestKeepBothSidesTakesARewriteAloneWithNothingAddedBesideIt(t *testing.T) {
	same(t, KeepBothSides(conflict([]string{"- k", "- e1"}, []string{"- k", "- e1"}, []string{"- k", "- e1x"})),
		merged(took(SecondRewrite), "- k", "- e1x"))
}

func TestKeepBothSidesTakesAStretchBothSidesMadeIntoTheSameTextOnce(t *testing.T) {
	marked := conflict(
		[]string{"- fixed", "- k", "- m", "- e1"},
		[]string{"- typo", "- k", "- e1"},
		[]string{"- fixed", "- k", "- e1x"},
	)
	same(t, KeepBothSides(marked), merged(took(Additions, SecondRewrite, SharedRewrite), "- fixed", "- k", "- m", "- e1x"))
}

func TestKeepBothSidesKeepsTheLineEndsOfAFileOfCRLF(t *testing.T) {
	crlf := func(lines ...string) string {
		var b strings.Builder
		for _, l := range lines {
			b.WriteString(l + "\r\n")
		}
		return b.String()
	}
	marked := crlf("a", open, "- m", "- e1", base, "- e1", split, "- e1x", close, "z")
	same(t, KeepBothSides(marked), Outcome{Text: crlf("a", "- m", "- e1x", "z"), Took: took(Additions, SecondRewrite)})
	same(t, KeepBothSides(crlf(open, "- m", base, split, "- e", close)), Outcome{Text: crlf("- m", "- e"), Took: took(Additions)})
}

func TestKeepBothSidesStops(t *testing.T) {
	cases := []struct {
		name     string
		sections [3][]string
		reason   string
	}{
		{"both sides rewrote the same entry", [3][]string{{"- m", "- e1 by master"}, {"- e1"}, {"- e1 by the branch"}},
			"both sides changed or removed a line that was already there"},
		{"the branch rewrote an entry master removed", [3][]string{{"- m"}, {"- e1"}, {"- e1x"}},
			"both sides changed or removed a line that was already there"},
		{"master rewrote an entry the branch removed", [3][]string{{"- e1x", "- k"}, {"- e1", "- k"}, {"- k", "- e"}},
			"both sides changed or removed a line that was already there"},
		{"the same fix on both sides with lines added behind it on each", [3][]string{{"- fixed", "- m"}, {"- typo"}, {"- fixed", "- e"}},
			"both sides changed or removed a line that was already there"},
		{"each side rewrote an entry of its own, side by side", [3][]string{{"- b1x", "- b2"}, {"- b1", "- b2"}, {"- b1", "- b2x"}},
			"each side changed lines that were already there, next to the other's"},
		{"each side rewrote an entry of its own with a line both kept between", [3][]string{{"- b1x", "- k", "- m", "- b2"}, {"- b1", "- k", "- b2"}, {"- b1", "- k", "- b2x"}},
			"each side changed lines that were already there, next to the other's"},
		{"one side added between two entries the other rewrote", [3][]string{{"- e1", "- m", "- e2"}, {"- e1", "- e2"}, {"- e1x", "- e2x"}},
			"one side added lines between lines the other side rewrote"},
		{"a rewrite with a line added, before lines master added", [3][]string{{"- e1", "- m"}, {"- e1"}, {"- e1x", "- e3"}},
			"lines one side added beside its rewrite meet lines the other side added"},
		{"master's rewrite with a line added, behind lines the branch added", [3][]string{{"- m1x", "- m2"}, {"- m1"}, {"- e", "- m1"}},
			"lines one side added beside its rewrite meet lines the other side added"},
		{"a rewrite of as many lines as it replaces, before lines master added", [3][]string{{"- e1", "- e2", "- m"}, {"- e1", "- e2"}, {"- e2x", "- e3"}},
			"lines one side added beside its rewrite meet lines the other side added"},
		{"master's rewrite of as many lines as it replaces, behind lines the branch added", [3][]string{{"- m1x", "- m3"}, {"- m1", "- m2"}, {"- e", "- m1", "- m2"}},
			"lines one side added beside its rewrite meet lines the other side added"},
		{"a rewrite of fewer lines than it replaces, before lines master added", [3][]string{{"- e1", "- e2", "- m"}, {"- e1", "- e2"}, {"- e3"}},
			"lines one side added beside its rewrite meet lines the other side added"},
		{"a line one side added stands in the other's rewrite", [3][]string{{"- new", "- e1"}, {"- e1"}, {"- new", "- e1x"}},
			"a line one side added stands in what the other side rewrote"},
		{"a base line standing twice", [3][]string{{"- m", "", "- e1", ""}, {"", "- e1", ""}, {"", "- e1x", ""}},
			"a line stands twice among the lines that were already there"},
		{"a base line standing twice on one side", [3][]string{{"- e1", "- m", "- e1", "- e2"}, {"- e1", "- e2"}, {"- e1", "- e2x"}},
			"a line that was already there stands twice on one side"},
		{"a side moved a line", [3][]string{{"- e2", "- e1"}, {"- e1", "- e2"}, {"- e1", "- e2", "- e3"}},
			"one side moved a line that was already there"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			same(t, KeepBothSides(conflict(c.sections[0], c.sections[1], c.sections[2])), edited(c.reason))
		})
	}
}

func TestKeepBothSidesStopsOnOneConflictItMayNotResolveAmongOthersItMay(t *testing.T) {
	marked := conflict([]string{"- m", "- e1"}, []string{"- e1"}, []string{"- e1x"}) + conflict([]string{"- x"}, []string{"- e2"}, []string{"- y"})
	if KeepBothSides(marked).Merged() {
		t.Fatal("merged")
	}
}

type kv [][2]string

func obj(pairs kv) string {
	o := ojson.NewObject()
	for _, p := range pairs {
		o.Set(p[0], p[1])
	}
	data, _ := ojson.Marshal(o)
	return string(data)
}

func with(a kv, more ...[2]string) kv {
	return append(append(kv{}, a...), more...)
}

func ptr(s string) *string {
	return &s
}

var start = kv{{"a.one", "One"}, {"@a.one", "First"}, {"b.one", "B"}}

func TestMergeJSONKeysMergesKeysBothSidesAppendedAtTheEndTheFirstSidesFirst(t *testing.T) {
	got := MergeJSONKeys(ptr(obj(start)), obj(with(start, [2]string{"m.x", "Master"})), obj(with(start, [2]string{"f.x", "Feature"}, [2]string{"@f.x", "Note"})))
	same(t, got, Outcome{Text: obj(with(start, [2]string{"m.x", "Master"}, [2]string{"f.x", "Feature"}, [2]string{"@f.x", "Note"})), Took: took(Additions)})
	var v map[string]string
	if err := json.Unmarshal([]byte(got.Text), &v); err != nil {
		t.Fatal(err)
	}
}

func TestMergeJSONKeysPlacesAKeyAppendedUnderItsFeatureAfterTheKeyItFollows(t *testing.T) {
	ours := kv{{"a.one", "One"}, {"@a.one", "First"}, {"a.m", "M"}, {"b.one", "B"}}
	theirs := kv{{"a.one", "One"}, {"@a.one", "First"}, {"a.f", "F"}, {"@a.f", "N"}, {"b.one", "B"}}
	got := MergeJSONKeys(ptr(obj(start)), obj(ours), obj(theirs))
	o := parseObject(got.Text)
	want := []string{"a.one", "@a.one", "a.m", "a.f", "@a.f", "b.one"}
	if o == nil || !reflect.DeepEqual(o.Keys(), want) {
		t.Fatal(got)
	}
}

func TestMergeJSONKeysTakesAKeyBothSidesAddedWithOneValueOnce(t *testing.T) {
	side := obj(with(start, [2]string{"c.x", "Same"}))
	same(t, MergeJSONKeys(ptr(obj(start)), side, side), Outcome{Text: side, Took: took(Additions)})
}

func TestMergeJSONKeysStopsOnTheSameKeyAddedWithTwoValues(t *testing.T) {
	got := MergeJSONKeys(ptr(obj(start)), obj(with(start, [2]string{"c.x", "Master"})), obj(with(start, [2]string{"c.x", "Feature"})))
	same(t, got, edited(`both sides added the key "c.x" with another value`))
}

func TestMergeJSONKeysTakesTheStateOfTheOneSideThatChangedOrRemovedAKey(t *testing.T) {
	got := MergeJSONKeys(ptr(obj(start)),
		obj(kv{{"a.one", "Uno"}, {"@a.one", "First"}, {"b.one", "B"}, {"m.x", "M"}}),
		obj(kv{{"a.one", "One"}, {"@a.one", "First"}, {"f.x", "F"}}))
	same(t, got, Outcome{Text: obj(kv{{"a.one", "Uno"}, {"@a.one", "First"}, {"m.x", "M"}, {"f.x", "F"}}), Took: took(Additions, FirstRewrite, SecondRewrite)})
}

func TestMergeJSONKeysTakesAKeyALaterCommitChangedBehindTheKeysMasterAdded(t *testing.T) {
	parent := with(start, [2]string{"f.x", "Feature"}, [2]string{"f.y", "Other"})
	onto := with(start, [2]string{"m.x", "Master"}, [2]string{"f.x", "Feature"}, [2]string{"f.y", "Other"})
	later := with(start, [2]string{"f.x", "Feature, rewritten"}, [2]string{"f.z", "Third"})
	same(t, MergeJSONKeys(ptr(obj(parent)), obj(onto), obj(later)),
		Outcome{Text: obj(with(start, [2]string{"m.x", "Master"}, [2]string{"f.x", "Feature, rewritten"}, [2]string{"f.z", "Third"})), Took: took(Additions, SecondRewrite)})
}

func TestMergeJSONKeysTakesAChangeBothSidesMadeAlikeOnce(t *testing.T) {
	fixed := obj(kv{{"a.one", "One"}, {"@a.one", "First"}, {"b.one", "Fixed"}})
	same(t, MergeJSONKeys(ptr(obj(start)), fixed, fixed), Outcome{Text: fixed, Took: took(SharedRewrite)})
}

func TestMergeJSONKeysStopsWhenBothSidesChangedOneKeyOrOneChangedWhatTheOtherRemoved(t *testing.T) {
	changed := func(v string) string { return obj(kv{{"a.one", "One"}, {"@a.one", "First"}, {"b.one", v}}) }
	removed := obj(kv{{"a.one", "One"}, {"@a.one", "First"}})
	for _, c := range [][2]string{{changed("X"), changed("Y")}, {changed("X"), removed}, {removed, changed("X")}} {
		if MergeJSONKeys(ptr(obj(start)), c[0], c[1]).Merged() {
			t.Fatal(c)
		}
	}
}

func TestMergeJSONKeysMergesAFileBothSidesCreatedWithNoBase(t *testing.T) {
	same(t, MergeJSONKeys(nil, obj(kv{{"a", "1"}}), obj(kv{{"b", "2"}})), Outcome{Text: obj(kv{{"a", "1"}, {"b", "2"}}), Took: took(Additions)})
}

func TestMergeJSONKeysStopsOnAVersionThatIsNotAJSONObject(t *testing.T) {
	if MergeJSONKeys(ptr(obj(start)), "{ broken", obj(start)).Merged() || MergeJSONKeys(ptr(obj(start)), obj(start), "[]\n").Merged() {
		t.Fatal("merged")
	}
}

func TestMergeJSONKeysKeepsTheFilesIndentItsEscapesAndItsLackOfAFinalNewline(t *testing.T) {
	base := "{\n    \"a\": \"1\",\n    \"b\": \"caf\\u00e9\"\n}"
	ours := "{\n    \"a\": \"1\",\n    \"b\": \"caf\\u00e9\",\n    \"c\": \"3\"\n}"
	theirs := "{\n    \"a\": \"1\",\n    \"b\": \"caf\\u00e9\",\n    \"d\": \"4\"\n}"
	same(t, MergeJSONKeys(&base, ours, theirs), Outcome{Text: "{\n    \"a\": \"1\",\n    \"b\": \"caf\\u00e9\",\n    \"c\": \"3\",\n    \"d\": \"4\"\n}", Took: took(Additions)})
}

func TestMergeJSONKeysKeepsTabsCRLFLineEndsAndEachValueAsItsSideWroteIt(t *testing.T) {
	base := "{\r\n\t\"a\": \"é\"\r\n}\r\n"
	ours := "{\r\n\t\"a\": \"é\",\r\n\t\"m\": [1, 2.50]\r\n}\r\n"
	theirs := "{\r\n\t\"a\": \"é\",\r\n\t\"f\":{\"x\": \"\\u00e9\"}\r\n}\r\n"
	same(t, MergeJSONKeys(&base, ours, theirs), Outcome{Text: "{\r\n\t\"a\": \"é\",\r\n\t\"m\": [1, 2.50],\r\n\t\"f\":{\"x\": \"\\u00e9\"}\r\n}\r\n", Took: took(Additions)})
}

func TestMergeJSONKeysTakesTheOtherSidesTextOfAKeyOnlyItChanged(t *testing.T) {
	base := "{\n  \"a\": \"1\",\n  \"b\": \"2\"\n}\n"
	ours := "{\n  \"a\": \"1\",\n  \"b\": \"2\",\n  \"m\": \"M\"\n}\n"
	theirs := "{\n  \"a\": \"caf\\u00e9\",\n  \"b\": \"2\"\n}\n"
	same(t, MergeJSONKeys(&base, ours, theirs), Outcome{Text: "{\n  \"a\": \"caf\\u00e9\",\n  \"b\": \"2\",\n  \"m\": \"M\"\n}\n", Took: took(Additions, SecondRewrite)})
}
