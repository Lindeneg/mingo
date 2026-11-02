package team

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/Lindeneg/mingo/internal/ojson"
)

const MarkerSize = 40

var (
	oursMarker   = strings.Repeat("<", MarkerSize)
	baseMarker   = strings.Repeat("|", MarkerSize)
	splitMarker  = strings.Repeat("=", MarkerSize)
	theirsMarker = strings.Repeat(">", MarkerSize)
)

type Taken string

const (
	Additions     Taken = "additions"
	FirstRewrite  Taken = "first-rewrite"
	SecondRewrite Taken = "second-rewrite"
	SharedRewrite Taken = "shared-rewrite"
)

var takenOrder = []Taken{Additions, FirstRewrite, SecondRewrite, SharedRewrite}

type Outcome struct {
	Text   string
	Took   []Taken
	Reason string
}

func (o Outcome) Merged() bool {
	return o.Reason == ""
}

func edited(reason string) Outcome {
	return Outcome{Reason: reason}
}

type takenSet map[Taken]bool

func (t takenSet) inOrder() []Taken {
	out := []Taken{}
	for _, k := range takenOrder {
		if t[k] {
			out = append(out, k)
		}
	}
	return out
}

func isMarker(line, marker string) bool {
	bare := strings.TrimSuffix(line, "\r")
	return bare == marker || strings.HasPrefix(bare, marker+" ")
}

func indexFrom(lines []string, line string, from int) int {
	for i := from; i < len(lines); i++ {
		if lines[i] == line {
			return i
		}
	}
	return -1
}

func joinAdditions(ours, theirs []string) []string {
	shorter := min(len(ours), len(theirs))
	head := 0
	for head < shorter && ours[head] == theirs[head] {
		head++
	}
	tail := 0
	for tail < shorter-head && ours[len(ours)-1-tail] == theirs[len(theirs)-1-tail] {
		tail++
	}
	out := slices.Clone(ours[:len(ours)-tail])
	return append(out, theirs[head:]...)
}

func placeBaseLines(base, side []string) ([]int, string) {
	places := make([]int, len(base))
	for i, line := range base {
		places[i] = indexFrom(side, line, 0)
	}
	last := -1
	for i, place := range places {
		if place == -1 {
			continue
		}
		if indexFrom(side, base[i], place+1) != -1 {
			return nil, "a line that was already there stands twice on one side"
		}
		if place < last {
			return nil, "one side moved a line that was already there"
		}
		last = place
	}
	return places, ""
}

// a stretch where one side kept every base line and the other kept none
func takeRewrite(changed, keeper, rewrite []string, rewriter Taken) ([]string, []Taken, string) {
	added := [][]string{{}}
	for _, line := range keeper {
		if next := len(added) - 1; next < len(changed) && line == changed[next] {
			added = append(added, []string{})
		} else {
			added[len(added)-1] = append(added[len(added)-1], line)
		}
	}
	before := added[0]
	var behind, between []string
	if len(added) > 1 {
		behind = added[len(added)-1]
		for _, a := range added[1 : len(added)-1] {
			between = append(between, a...)
		}
	}
	if len(rewrite) > 0 && len(between) > 0 {
		return nil, nil, "one side added lines between lines the other side rewrote"
	}
	for _, group := range [][]string{before, between, behind} {
		for _, line := range group {
			if slices.Contains(rewrite, line) {
				return nil, nil, "a line one side added stands in what the other side rewrote"
			}
		}
	}
	outOfOrder := before
	if rewriter == SecondRewrite {
		outOfOrder = behind
	}
	lineForLine := len(rewrite) == 0 || (len(rewrite) == 1 && len(changed) == 1)
	if !lineForLine && len(outOfOrder) > 0 {
		return nil, nil, "lines one side added beside its rewrite meet lines the other side added"
	}
	took := []Taken{rewriter}
	if len(before)+len(between)+len(behind) > 0 || len(rewrite) > len(changed) {
		took = append(took, Additions)
	}
	var lines []string
	for _, group := range [][]string{before, between, rewrite, behind} {
		lines = append(lines, group...)
	}
	return lines, took, ""
}

func resolveConflict(ours, base, theirs []string) ([]string, []Taken, string) {
	if len(base) == 0 {
		return joinAdditions(ours, theirs), []Taken{Additions}, ""
	}
	seen := map[string]bool{}
	for _, line := range base {
		if seen[line] {
			return nil, nil, "a line stands twice among the lines that were already there"
		}
		seen[line] = true
	}
	inOurs, reason := placeBaseLines(base, ours)
	if reason != "" {
		return nil, nil, reason
	}
	inTheirs, reason := placeBaseLines(base, theirs)
	if reason != "" {
		return nil, nil, reason
	}
	keptByOurs := func(at int) bool { return inOurs[at] != -1 }
	keptByTheirs := func(at int) bool { return inTheirs[at] != -1 }
	var lines []string
	took := takenSet{}
	var rewriter Taken
	from, oursFrom, theirsFrom := 0, 0, 0
	for index := 0; index <= len(base); index++ {
		end := index == len(base)
		oursAt, theirsAt := len(ours), len(theirs)
		if !end {
			oursAt, theirsAt = inOurs[index], inTheirs[index]
		}
		if oursAt == -1 || theirsAt == -1 {
			continue
		}
		changed := base[from:index]
		oursStretch := ours[oursFrom:oursAt]
		theirsStretch := theirs[theirsFrom:theirsAt]
		switch {
		case len(changed) == 0:
			if len(oursStretch)+len(theirsStretch) > 0 {
				lines = append(lines, joinAdditions(oursStretch, theirsStretch)...)
				took[Additions] = true
			}
		case slices.Equal(oursStretch, theirsStretch):
			lines = append(lines, oursStretch...)
			took[SharedRewrite] = true
		default:
			allOurs, allTheirs := true, true
			for at := from; at < index; at++ {
				if !keptByOurs(at) && !keptByTheirs(at) {
					return nil, nil, "both sides changed or removed a line that was already there"
				}
				allOurs = allOurs && keptByOurs(at)
				allTheirs = allTheirs && keptByTheirs(at)
			}
			by := FirstRewrite
			if allOurs {
				by = SecondRewrite
			}
			if (by == FirstRewrite && !allTheirs) || (rewriter != "" && rewriter != by) {
				return nil, nil, "each side changed lines that were already there, next to the other's"
			}
			rewriter = by
			keeper, rewrite := theirsStretch, oursStretch
			if by == SecondRewrite {
				keeper, rewrite = oursStretch, theirsStretch
			}
			taken, what, reason := takeRewrite(changed, keeper, rewrite, by)
			if reason != "" {
				return nil, nil, reason
			}
			lines = append(lines, taken...)
			for _, w := range what {
				took[w] = true
			}
		}
		if !end {
			lines = append(lines, base[index])
		}
		from, oursFrom, theirsFrom = index+1, oursAt+1, theirsAt+1
	}
	return lines, took.inOrder(), ""
}

func KeepBothSides(marked string) Outcome {
	var kept, ours, base, theirs []string
	took := takenSet{}
	section := "outside"
	for _, line := range strings.Split(marked, "\n") {
		switch {
		case section == "outside" && isMarker(line, oursMarker):
			section = "ours"
		case section == "ours" && isMarker(line, baseMarker):
			section = "base"
		case section == "base" && isMarker(line, splitMarker):
			section = "theirs"
		case section == "theirs" && isMarker(line, theirsMarker):
			lines, what, reason := resolveConflict(ours, base, theirs)
			if reason != "" {
				return edited(reason)
			}
			kept = append(kept, lines...)
			for _, w := range what {
				took[w] = true
			}
			ours, base, theirs = nil, nil, nil
			section = "outside"
		case section == "outside":
			kept = append(kept, line)
		case section == "ours":
			ours = append(ours, line)
		case section == "base":
			base = append(base, line)
		default:
			theirs = append(theirs, line)
		}
	}
	if section != "outside" {
		return edited("the conflict markers do not pair up")
	}
	return Outcome{Text: strings.Join(kept, "\n"), Took: took.inOrder()}
}

func parseObject(text string) *ojson.Object {
	v, err := ojson.Parse([]byte(text))
	if err != nil {
		return nil
	}
	o, _ := v.(*ojson.Object)
	return o
}

func jsonText(v any) []byte {
	data, err := ojson.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func sameKey(a *ojson.Object, ka string, b *ojson.Object, kb string) bool {
	va, hasA := a.Get(ka)
	vb, hasB := b.Get(kb)
	return hasA == hasB && bytes.Equal(jsonText(va), jsonText(vb))
}

func has(o *ojson.Object, key string) bool {
	_, ok := o.Get(key)
	return ok
}

// base is nil when both sides created the file
func MergeJSONKeys(base *string, ours, theirs string) Outcome {
	baseObject := ojson.NewObject()
	if base != nil {
		baseObject = parseObject(*base)
	}
	oursObject, theirsObject := parseObject(ours), parseObject(theirs)
	if baseObject == nil || oursObject == nil || theirsObject == nil {
		return edited("a version of the file is not a JSON object")
	}
	order := slices.Clone(oursObject.Keys())
	values := map[string]any{}
	for _, k := range order {
		values[k], _ = oursObject.Get(k)
	}
	took := takenSet{}
	for _, k := range order {
		if !has(baseObject, k) {
			took[Additions] = true
		}
	}

	for _, key := range baseObject.Keys() {
		oursSame := has(oursObject, key) && sameKey(oursObject, key, baseObject, key)
		theirsSame := has(theirsObject, key) && sameKey(theirsObject, key, baseObject, key)
		if theirsSame {
			if !oursSame {
				took[FirstRewrite] = true
			}
			continue
		}
		if !oursSame && !sameKey(oursObject, key, theirsObject, key) {
			return edited(fmt.Sprintf("both sides changed the key %q", key))
		}
		if oursSame {
			took[SecondRewrite] = true
		} else {
			took[SharedRewrite] = true
		}
		if v, ok := theirsObject.Get(key); ok {
			values[key] = v
		} else if _, ok := values[key]; ok {
			delete(values, key)
			order = slices.Delete(order, slices.Index(order, key), slices.Index(order, key)+1)
		}
	}

	theirsKeys := theirsObject.Keys()
	for index, key := range theirsKeys {
		if has(baseObject, key) {
			continue
		}
		took[Additions] = true
		if has(oursObject, key) {
			if !sameKey(oursObject, key, theirsObject, key) {
				return edited(fmt.Sprintf("both sides added the key %q with another value", key))
			}
			continue
		}
		at := 0
		for i := index - 1; i >= 0; i-- {
			if _, ok := values[theirsKeys[i]]; ok {
				at = slices.Index(order, theirsKeys[i]) + 1
				break
			}
		}
		for at < len(order) && !has(baseObject, order[at]) && !has(theirsObject, order[at]) {
			at++
		}
		order = slices.Insert(order, at, key)
		values[key], _ = theirsObject.Get(key)
	}

	merged := ojson.NewObject()
	for _, k := range order {
		merged.Set(k, values[k])
	}
	return Outcome{Text: string(jsonText(merged)), Took: took.inOrder()}
}
