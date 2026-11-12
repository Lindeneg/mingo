// Package sys holds what differs between operating systems: free memory, process start times,
// terminals and the boot service.
package sys

import (
	"fmt"
	"runtime"
)

func NotBuilt(what string) error {
	return fmt.Errorf("%s is not built yet on %s", what, runtime.GOOS)
}
