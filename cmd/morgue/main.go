package main

import (
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wailsapp/wails/v3/pkg/application"

	morgue "github.com/UberMorgott/morgue"
	"github.com/UberMorgott/morgue/internal/api"
	"github.com/UberMorgott/morgue/internal/cli"
	"github.com/UberMorgott/morgue/internal/selfupdate"
	"github.com/UberMorgott/morgue/internal/services"
	"github.com/UberMorgott/morgue/internal/util"
	"github.com/UberMorgott/morgue/internal/webview2"
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
	// Check WebView2 availability
	version, isLocal := webview2.CheckAvailable()
	browserPath := ""

	if version == "" {
		switch webview2.ShowInstallDialog() {
		case webview2.ResultClose:
			os.Exit(0)
		case webview2.ResultSystem:
			if err := webview2.InstallSystem(); err != nil {
				webview2.ShowError(fmt.Sprintf("System install failed:\n%v", err))
				os.Exit(1)
			}
			// Re-check after installation
			// Note: MORGUE_TEST_NO_WEBVIEW2=1 will cause this to always fail — unset it for real testing
			version, isLocal = webview2.CheckAvailable()
			if version == "" {
				webview2.ShowError("WebView2 still not available after installation. Try restarting the application.")
				os.Exit(1)
			}
		case webview2.ResultPortable:
			if err := webview2.InstallPortable(); err != nil {
				webview2.ShowError(fmt.Sprintf("Portable install failed:\n%v", err))
				os.Exit(1)
			}
			browserPath = webview2.LocalRuntimePath()
		}
	} else if isLocal {
		browserPath = webview2.LocalRuntimePath()
	}

	// Create services once — shared between Wails and HTTP API
	toolsSvc := services.NewToolsService(Version)
	pipelineSvc := services.NewPipelineService()
	configSvc := &services.ConfigService{}
	reconSvc := &services.ReconService{}
	updateSvc := &services.UpdateService{Version: Version, Commit: Commit}

	// Declare apiSrv early so OnShutdown closure can capture it
	var apiSrv *api.Server

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
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(util.BaseDir(), ".webview2"),
			WebviewBrowserPath:  browserPath,
		},
		OnShutdown: func() {
			pipelineSvc.Stop()
			if apiSrv != nil {
				apiSrv.Stop()
			}
		},
	})

	// HTTP API for hybrid mode
	apiSrv = api.NewServer(pipelineSvc, toolsSvc, configSvc, reconSvc)
	apiSrv.HookEvents(app)
	if err := apiSrv.Start(); err != nil {
		log.Printf("api server: %v", err)
	}

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
		Short: "Binary Decompilation Orchestrator",
		Long: `Morgue — Binary Decompilation Orchestrator

Modes:
  morgue                Launch GUI (hybrid mode — AI can control via 'morgue api')
  morgue run <file>     Headless decompilation (CLI only)
  morgue api <command>  Control running GUI instance

When GUI is running, AI agents can use 'morgue api' to control it.
The user sees all changes in the application window in real-time.`,
	}

	root.AddCommand(runCmd())
	root.AddCommand(infoCmd())
	root.AddCommand(toolsCmd())
	root.AddCommand(versionCmd())
	root.AddCommand(selfUpdateCmd())
	root.AddCommand(apiCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [target]",
		Short: "Run decompilation pipeline on a target binary",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.Join(args, " ")
			output, _ := cmd.Flags().GetString("output")
			watch, _ := cmd.Flags().GetBool("watch")
			quiet, _ := cmd.Flags().GetBool("quiet")
			recipe, _ := cmd.Flags().GetString("recipe")
			noSkip, _ := cmd.Flags().GetBool("no-skip")
			exclude, _ := cmd.Flags().GetStringSlice("exclude")

			return cli.Run(cli.RunOptions{
				Target:  target,
				Output:  output,
				Recipe:  recipe,
				NoSkip:  noSkip,
				Exclude: exclude,
				Watch:   watch,
				Quiet:   quiet,
			})
		},
	}

	cmd.Flags().StringP("output", "o", "./decompiled", "Output directory")
	cmd.Flags().Bool("watch", false, "Show TUI progress in stderr")
	cmd.Flags().BoolP("quiet", "q", false, "Suppress stderr output, only emit JSON to stdout")
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
		Use:   "install [name]",
		Short: "Download and install required tools (all missing, or a specific one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return cli.ToolsInstallOne(args[0])
			}
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

func apiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Interact with the running GUI's HTTP API",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Get GUI status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.APIStatus()
		},
	})

	apiRunCmd := &cobra.Command{
		Use:   "run <path>",
		Short: "Send a decompilation job to the GUI",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.Join(args, " ")
			output, _ := cmd.Flags().GetString("output")
			wait, _ := cmd.Flags().GetBool("wait")
			if wait {
				return cli.APIRunWait(target, output)
			}
			return cli.APIRun(target, output)
		},
	}
	apiRunCmd.Flags().StringP("output", "o", "", "Output directory")
	apiRunCmd.Flags().Bool("wait", false, "Wait until the pipeline completes, showing progress")
	cmd.AddCommand(apiRunCmd)

	apiToolsCmd := &cobra.Command{
		Use:   "tools",
		Short: "List tools via GUI API",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, _ := cmd.Flags().GetBool("wait")
			if wait {
				return cli.APIToolsWait()
			}
			return cli.APITools()
		},
	}
	apiToolsCmd.Flags().Bool("wait", false, "Wait until all tool operations finish")

	apiToolsCmd.AddCommand(&cobra.Command{
		Use:   "install [name]",
		Short: "Install a tool (or all missing tools) via GUI API",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return cli.APIToolsInstall(name)
		},
	})

	apiToolsCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a tool via GUI API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.APIToolsDelete(args[0])
		},
	})

	cmd.AddCommand(apiToolsCmd)

	apiSettingsCmd := &cobra.Command{
		Use:   "settings",
		Short: "Get current settings via GUI API",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.APISettings()
		},
	}

	apiSettingsCmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Update a setting via GUI API",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.APISettingsSet(args[0], args[1])
		},
	})

	cmd.AddCommand(apiSettingsCmd)

	return cmd
}

func infoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [file]",
		Short: "Show binary information without decompiling",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			return cli.Info(cli.InfoOptions{
				Target: args[0],
				Format: format,
			})
		},
	}
	cmd.Flags().String("format", "json", "Output format: json or text")
	return cmd
}
