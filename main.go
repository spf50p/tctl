package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.5.0"

var configPath string

func main() {
	root := &cobra.Command{
		Use:           "tctl",
		Short:         "tctl CLI",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&configPath, "conf", "c", ".tctl.yaml", "config path")
	root.AddCommand(collectCmd())
	root.AddCommand(dcsCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
