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
	// Cap process memory before doing any real work, so both the CLI and the GUI
	// are protected against a runaway allocation (the function-split OOM that
	// froze the user's machine). Applied once, here, for every code path.
	applyMemoryCap()

	// Portable app: all runtime state (temp, tool homes, caches, API token)
	// lives in <exe dir>/data. Refuse to run rather than fall back to system
	// dirs when that folder is not writable.
	cleanup, err := util.InitPortable()
	if err != nil {
		if len(os.Args) > 1 {
			attachParentConsole()
			fmt.Fprintln(os.Stderr, "morgue:", err)
		} else {
			webview2.ShowError(err.Error())
		}
		os.Exit(1)
	}
	exitCleanup = cleanup

	// Drop the binary displaced by a previous self-update, if any. Windows
	// cannot delete a running .exe, so the swap leaves <exe>.old behind for the
	// next start to clean up.
	selfupdate.CleanupOld()

	// If CLI args provided → cobra
	if len(os.Args) > 1 {
		exit(runCLI())
	}

	// No args → Wails GUI
	runGUI()
	exit(0)
}

// exitCleanup removes this process's temp dir (set from util.InitPortable).
var exitCleanup = func() {}

// exit runs exitCleanup and terminates with code. Use it instead of os.Exit
// so the per-process temp dir under <exe dir>/data/tmp does not linger.
func exit(code int) {
	exitCleanup()
	os.Exit(code)
}

// applyMemoryCap installs a Windows Job Object memory limit on the morgue
// process. The cap is min(4 GiB, ~75% of physical RAM) and is never set above
// physical RAM; with it in force, a runaway allocation fails (the allocator
// errors) instead of thrashing the whole machine. No-op on non-Windows.
func applyMemoryCap() {
	const (
		hardCap  uintptr = 4 << 30 // 4 GiB absolute ceiling
		fallback uintptr = 2 << 30 // used if physical RAM is unknown
		safePct          = 75      // reserve at most this % of physical RAM
	)

	limit := hardCap
	if phys := util.TotalPhysicalMemoryBytes(); phys > 0 {
		// 75% of physical RAM is always < physical, so the cap is never set at
		// or above total RAM. Take whichever is smaller: that fraction or 4 GiB.
		if safe := uintptr(phys / 100 * safePct); safe < limit {
			limit = safe
		}
	} else if limit > fallback {
		// Physical RAM unknown (e.g. probe failed): stay conservative.
		limit = fallback
	}
	if limit == 0 {
		return
	}

	if err := util.LimitProcessMemory(limit); err != nil {
		log.Printf("memory cap: could not apply %d MiB process limit: %v", limit>>20, err)
		return
	}
	log.Printf("memory cap: limited process to %d MiB", limit>>20)
}

func runGUI() {
	// Check WebView2 availability
	version, isLocal := webview2.CheckAvailable()
	browserPath := ""

	if version == "" {
		switch webview2.ShowInstallDialog() {
		case webview2.ResultClose:
			exit(0)
		case webview2.ResultSystem:
			if err := webview2.InstallSystem(); err != nil {
				webview2.ShowError(fmt.Sprintf("System install failed:\n%v", err))
				exit(1)
			}
			// Re-check after installation
			// Note: MORGUE_TEST_NO_WEBVIEW2=1 will cause this to always fail — unset it for real testing
			version, isLocal = webview2.CheckAvailable()
			if version == "" {
				webview2.ShowError("WebView2 still not available after installation. Try restarting the application.")
				exit(1)
			}
			if isLocal {
				browserPath = webview2.LocalRuntimePath()
			}
		case webview2.ResultPortable:
			if err := webview2.InstallPortable(); err != nil {
				webview2.ShowError(fmt.Sprintf("Portable install failed:\n%v", err))
				exit(1)
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
	instructionsSvc := &services.InstructionsService{}

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
			application.NewService(instructionsSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(morgue.Assets),
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(util.BaseDir(), ".webview2"),
			WebviewBrowserPath:  browserPath,
		},
		OnShutdown: func() {
			if err := pipelineSvc.Stop(); err != nil {
				log.Printf("shutdown: stop pipeline: %v", err)
			}
			if apiSrv != nil {
				if err := apiSrv.Stop(); err != nil {
					log.Printf("shutdown: stop api server: %v", err)
				}
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
		log.Print(err)
		exit(1)
	}
}

func runCLI() int {
	// The release binary is linked with -H=windowsgui (no console), so a CLI
	// invocation from a terminal would print nothing. Reattach to the parent
	// console first. No-op/harmless in console-subsystem dev builds and when
	// launched detached.
	attachParentConsole()

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
		// Cobra validates positional args and parses flags BEFORE persistent
		// pre-run hooks, so usage is still printed for arg/flag mistakes. Once
		// we reach here the invocation is well-formed: a RunE error is a
		// runtime failure and dumping usage text after it is just noise.
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SilenceUsage = true
		},
	}

	root.AddCommand(runCmd())
	root.AddCommand(gamedataCmd())
	root.AddCommand(infoCmd())
	root.AddCommand(toolsCmd())
	root.AddCommand(versionCmd())
	root.AddCommand(selfUpdateCmd())
	root.AddCommand(apiCmd())

	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}

func runCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [target]",
		Short: "Run decompilation pipeline on a target binary",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.Join(args, " ")
			watch, _ := cmd.Flags().GetBool("watch")
			forceWatch, _ := cmd.Flags().GetBool("force-watch")
			quiet, _ := cmd.Flags().GetBool("quiet")

			// Build the run request from flags so an explicitly provided
			// --allow-dynamic=false (or --no-skip=false etc.) is preserved as a
			// deliberate false, while an omitted bool flag stays nil and inherits
			// the loaded config default during admission (plan C1: explicit >
			// config > built-in). This is the same request admission shared with
			// the HTTP API and the Wails GUI.
			req := cli.RequestFromFlags(cmd, target)
			return cli.Run(cli.RunOptions{
				Watch:      watch || forceWatch,
				ForceWatch: forceWatch,
				Quiet:      quiet,
				Request:    &req,
			})
		},
	}

	// Run-request flags (names/defaults match the HTTP RunRequest one-to-one).
	cli.AddRunFlags(cmd)
	// CLI-only rendering flags, not part of the run request.
	cmd.Flags().Bool("watch", false, "Show TUI progress in stderr")
	cmd.Flags().Bool("force-watch", false, "Force the watch TUI even when stderr is not a terminal")
	cmd.Flags().BoolP("quiet", "q", false, "Suppress stderr output, only emit JSON to stdout")

	return cmd
}

func gamedataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gamedata [exportDir]",
		Short: "Organize an existing AssetRipper Unity export into a game-data tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, _ := cmd.Flags().GetString("out")
			inventory, _ := cmd.Flags().GetString("inventory")
			full, _ := cmd.Flags().GetString("full")
			return cli.GameData(cli.GameDataOptions{
				ExportDir: args[0],
				Out:       out,
				Inventory: inventory,
				Full:      full,
			})
		},
	}
	cmd.Flags().StringP("out", "o", "", "Output directory for the organized game-data tree (required)")
	cmd.Flags().String("inventory", "", "AssetStudio inventory CSV to merge (optional)")
	cmd.Flags().String("full", "", "Separate full-export dir for prefabs/scenes (optional)")
	if err := cmd.MarkFlagRequired("out"); err != nil {
		log.Printf("gamedata: mark --out required: %v", err)
	}
	return cmd
}

func toolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Manage external tool dependencies",
	}

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Check which tools are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			updates, _ := cmd.Flags().GetBool("updates")
			return cli.ToolsCheck(updates)
		},
	}
	checkCmd.Flags().BoolP("updates", "u", false, "Also query upstream versions and show available updates (network)")
	cmd.AddCommand(checkCmd)

	installCmd := &cobra.Command{
		Use:   "install [name]",
		Short: "Download and install required tools (all missing, or a specific one)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")
			if len(args) == 1 {
				return cli.ToolsInstallOne(cmd.Context(), args[0], force)
			}
			return cli.ToolsInstall(cmd.Context(), force)
		},
	}
	installCmd.Flags().BoolP("force", "f", false, "Reinstall/update through a staged replacement with rollback on failure")
	cmd.AddCommand(installCmd)

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
			// CLI path: no progress callback, no auto-relaunch — just print the
			// restart message (handled inside Update).
			return selfupdate.Update(Version, nil)
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
	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Cancel the GUI's active decompilation run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.APIStop()
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
