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

	fmt.Printf("Installing %d tools...\n", len(needed))
	for _, name := range needed {
		fmt.Printf("  Installing %s... ", name)
		version, err := mgr.Install(name, nil)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}
		fmt.Printf("OK (%s)\n", version)
	}

	return nil
}
