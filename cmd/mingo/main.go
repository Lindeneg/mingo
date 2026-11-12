// Command mingo runs agent sessions for every project on a machine, one tmux session per role.
package main

import (
	"os"

	"github.com/Lindeneg/mingo/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
