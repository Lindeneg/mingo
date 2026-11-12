//go:build !linux && !darwin

package sys

func FreeMB() (int, error) {
	return 0, NotBuilt("reading the free memory")
}

func ProcStart(pid int) (string, error) {
	return "", NotBuilt("reading a process's start time")
}

func IsTerminal(fd uintptr) bool {
	return false
}

func ServiceEnable(home, project string, unit []byte) error {
	return NotBuilt("the boot service")
}

func ServiceDisable(home, project string) error {
	return NotBuilt("the boot service")
}

func SetEcho(fd uintptr, on bool) error {
	return NotBuilt("hiding what is typed")
}
