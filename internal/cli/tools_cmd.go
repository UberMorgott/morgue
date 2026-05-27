package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// ToolsCheck prints a table of all tools and their installation status.
func ToolsCheck() error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tCATEGORY\tINSTALLED\tVERSION\tPATH")
	fmt.Fprintln(w, "----\t--------\t---------\t-------\t----")

	for _, def := range tools.Registry {
		status := mgr.Check(def.Name)
		installed := "no"
		if status.Installed {
			installed = "yes"
		}
		version := status.Version
		if version == "" {
			version = "-"
		}
		path := status.Path
		if !status.Installed {
			path = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			def.Name, def.Category, installed, version, path)
	}

	return w.Flush()
}

// cliCallbacks returns InstallCallbacks that print progress to stderr.
func cliCallbacks() *tools.InstallCallbacks {
	return &tools.InstallCallbacks{
		OnProgress: func(tool string, bytesDown, bytesTotal int64) {
			if bytesTotal > 0 {
				pct := int(bytesDown * 100 / bytesTotal)
				downMB := bytesDown / (1024 * 1024)
				totalMB := bytesTotal / (1024 * 1024)
				fmt.Fprintf(os.Stderr, "\rInstalling %s... downloading %d%% (%dMB/%dMB)", tool, pct, downMB, totalMB)
			} else {
				downMB := bytesDown / (1024 * 1024)
				fmt.Fprintf(os.Stderr, "\rInstalling %s... downloading %dMB", tool, downMB)
			}
		},
		OnExtract: func(tool string) {
			fmt.Fprintf(os.Stderr, "\rInstalling %s... extracting...                    ", tool)
		},
	}
}

// ToolsInstall installs all missing tools.
func ToolsInstall() error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	var needed []string
	for _, def := range tools.Registry {
		if !mgr.IsInstalled(def.Name) {
			needed = append(needed, def.Name)
		}
	}

	if len(needed) == 0 {
		fmt.Println("All tools are already installed.")
		return nil
	}

	cb := cliCallbacks()
	fmt.Printf("Installing %d tools...\n", len(needed))
	for _, name := range needed {
		version, err := mgr.Install(name, cb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rInstalling %s... FAILED: %v\n", name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "\rInstalling %s... done (%s)\n", name, version)
	}

	return nil
}

// ToolsInstallOne installs a single tool by name.
func ToolsInstallOne(name string) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	// Validate the tool name exists in registry
	if _, ok := tools.FindByName(name); !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	if mgr.IsInstalled(name) {
		fmt.Printf("Tool %s is already installed.\n", name)
		return nil
	}

	cb := cliCallbacks()
	version, err := mgr.Install(name, cb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\rInstalling %s... FAILED: %v\n", name, err)
		return err
	}
	fmt.Fprintf(os.Stderr, "\rInstalling %s... done (%s)\n", name, version)
	return nil
}
