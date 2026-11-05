//go:build linux || darwin

package sys

import "golang.org/x/sys/unix"

// keys arrive one at a time, unechoed, with ctrl c as a key, and the output still turns a newline
// into a carriage return and a line feed, so a prompt can redraw itself with plain lines
func MakeRaw(fd uintptr) (func() error, error) {
	old, err := unix.IoctlGetTermios(int(fd), ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Iflag &^= unix.ICRNL | unix.INLCR | unix.IGNCR | unix.IXON | unix.ISTRIP
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(int(fd), ioctlSetTermios, &t); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(int(fd), ioctlSetTermios, old) }, nil
}

// the terminal's width in columns, 0 when it does not say
func Width(fd uintptr) int {
	ws, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}
