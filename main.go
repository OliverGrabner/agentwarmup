// Command agentwarmup schedules a small early coding-agent request.
package main

import (
	"fmt"
	"os"

	"github.com/OliverGrabner/agentwarmup/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
