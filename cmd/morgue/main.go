package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
)

func main() {
	root := &cobra.Command{
		Use:   "morgue",
		Short: "Binary decompiler orchestrator",
		Long:  "Morgue — automated binary decompilation pipeline for .NET, Delphi, and native targets.",
	}

	root.AddCommand(runCmd())
	root.AddCommand(toolsCmd())
	root.AddCommand(versionCmd())
	root.AddCommand(selfUpdateCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [target]",
		Short: "Run decompilation pipeline on a target binary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("run: not yet implemented")
			return nil
		},
	}

	cmd.Flags().StringP("output", "o", "./decompiled", "Output directory")
	cmd.Flags().Bool("watch", false, "Show TUI progress in stderr")
	cmd.Flags().String("recipe", "", "Force specific recipe")
	cmd.Flags().Bool("no-skip", false, "Disable auto skip-list")
	cmd.Flags().StringSlice("exclude", nil, "Additional exclude patterns")

	return cmd
}

func toolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Manage external tool dependencies",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Check which tools are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("tools check: not yet implemented")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Download and install required tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("tools install: not yet implemented")
			return nil
		},
	})

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("morgue %s (%s)\n", Version, Commit)
		},
	}
}

func selfUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self-update",
		Short: "Update morgue to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("self-update: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Bool("check", false, "Check only, don't download")

	return cmd
}
