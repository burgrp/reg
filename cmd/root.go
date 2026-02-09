package cmd

import (
	"github.com/spf13/cobra"
)

func Execute() error {
	rootCmd := newRootCmd()
	return rootCmd.Execute()
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reg",
		Short: "reg is a CLI tool",
		Long:  `A CLI application built with Cobra`,
	}

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newProvideCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newSetCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newBrowseCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}
