package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var Version = "local-build"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  `Shows version of reg command line tool.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}
