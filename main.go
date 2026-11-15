// Command agent-desk is a desk where coding agents file questions and
// current state for the user, and the user reads and answers them.
package main

import (
	"fmt"
	"os"

	"github.com/NightMachinery/agent-desk/internal/cli"
)

func main() {
	app, err := cli.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-desk: %v\n", err)
		os.Exit(1)
	}
	os.Exit(app.Run(os.Args[1:]))
}
