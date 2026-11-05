//go:build !linux && !darwin

package sys

func MakeRaw(fd uintptr) (func() error, error) {
	return nil, NotBuilt("reading keys one at a time")
}

func Width(fd uintptr) int {
	return 0
}
