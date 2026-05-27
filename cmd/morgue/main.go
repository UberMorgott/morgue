package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/wailsapp/wails/v3/pkg/application"

	morgue "github.com/UberMorgott/morgue"
	"github.com/UberMorgott/morgue/internal/api"
	"github.com/UberMorgott/morgue/internal/cli"
	"github.com/UberMorgott/morgue/internal/selfupdate"
	"github.com/UberMorgott/morgue/internal/services"
)

//go:embed appicon.png
var appIcon []byte

var (
	Version = "dev"
	Commit  = "none"
)

func main() {
	// If CLI args provided → cobra
	if len(os.Args) > 1 {
		runCLI()
		return
	}

	// No args → Wails GUI
	runGUI()
}

func runGUI() {
	// Create services once — shared between Wails and HTTP API
	toolsSvc := services.NewToolsService(Version)
	pipelineSvc := services.NewPipelineService()
	configSvc := &services.ConfigService{}
	reconSvc := &services.ReconService{}
	updateSvc := &services.UpdateService{Version: Version}

	app := application.New(application.Options{
		Name:        "Morgue",
		Description: "Binary decompiler orchestrator",
		Services: []application.Service{
			application.NewService(reconSvc),
			application.NewService(pipelineSvc),
			application.NewService(toolsSvc),
			application.NewService(configSvc),
			application.NewService(updateSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(morgue.Assets),
		},
	})

	// HTTP API for hybrid mode
	apiSrv := api.NewServer(pipelineSvc, toolsSvc, configSvc, reconSvc)
	apiSrv.HookEvents(app)
	go apiSrv.Start()
	defer apiSrv.Stop()

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Morgue",
		Width:            1024,
		Height:           700,
		MinWidth:         800,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(10, 10, 15),
		URL:              "/",
	})

	// System tray
	tray := app.SystemTray.New()
	tray.SetTooltip("Morgue — Binary Decompiler")
	tray.SetIcon(appIcon)

	trayMenu := app.NewMenu()
	trayMenu.Add("Show/Hide").OnClick(func(ctx *application.Context) {
		if window.IsVisible() {
			window.Hide()
		} else {
			window.Show()
			window.Focus()
		}
	})
	trayMenu.AddSeparator()
	trayMenu.Add("Quit").OnClick(func(ctx *application.Context) {
		app.Quit()
	})

	tray.SetMenu(trayMenu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func runCLI() {
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
			output, _ := cmd.Flags().GetString("output")
			watch, _ := cmd.Flags().GetBool("watch")
			recipe, _ := cmd.Flags().GetString("recipe")
			noSkip, _ := cmd.Flags().GetBool("no-skip")
			exclude, _ := cmd.Flags().GetStringSlice("exclude")

			return cli.Run(cli.RunOptions{
				Target:  args[0],
				Output:  output,
				Recipe:  recipe,
				NoSkip:  noSkip,
				Exclude: exclude,
				Watch:   watch,
			})
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
			return cli.ToolsCheck()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Download and install required tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.ToolsInstall()
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
			checkOnly, _ := cmd.Flags().GetBool("check")
			if checkOnly {
				return selfupdate.Check(Version)
			}
			return selfupdate.Update(Version)
		},
	}

	cmd.Flags().Bool("check", false, "Check only, don't download")

	return cmd
}
