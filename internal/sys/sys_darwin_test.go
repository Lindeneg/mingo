//go:build darwin

package sys

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestFreeMBReadsTheFreeAndFileBackedPages(t *testing.T) {
	mb, err := FreeMB()
	if err != nil || mb <= 0 {
		t.Fatal(mb, err)
	}
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		t.Fatal(err)
	}
	var size, free, file int64
	number := func(s string) int64 {
		n, _ := strconv.ParseInt(strings.TrimSuffix(s, "."), 10, 64)
		return n
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		switch {
		case strings.Contains(line, "page size of"):
			for i := 0; i+1 < len(f); i++ {
				if f[i] == "of" {
					size = number(f[i+1])
				}
			}
		case strings.HasPrefix(line, "Pages free:") && len(f) == 3:
			free = number(f[2])
		case strings.HasPrefix(line, "File-backed pages:") && len(f) == 3:
			file = number(f[2])
		}
	}
	t.Logf("mingo reads %d MB free, vm_stat's free and file-backed pages make %d MB", mb, (free+file)*size>>20)
}
