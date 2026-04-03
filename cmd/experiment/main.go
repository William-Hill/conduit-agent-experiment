package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var _ = cobra.Command{}
var _ = viper.New()
var _ = yaml.Node{}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: conduit-experiment <command> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  index     Index the target repository")
		fmt.Fprintln(os.Stderr, "  run       Run a task against the target repository")
		fmt.Fprintln(os.Stderr, "  report    Generate a report for a completed run")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "index":
		fmt.Println("index: not yet implemented")
	case "run":
		fmt.Println("run: not yet implemented")
	case "report":
		fmt.Println("report: not yet implemented")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
