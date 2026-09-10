package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// ToolsCheck prints a table of all tools and their installation status.
// With updates=true it also queries upstream versions (network) and adds an UPDATE column.
func ToolsCheck(updates bool) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	statuses := make(map[string]tools.ToolStatus, len(tools.Registry))
	if updates {
		for _, st := range mgr.CheckAllWithUpdates() {
			statuses[st.Name] = st
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if updates {
		_, _ = fmt.Fprintln(w, "TOOL\tCATEGORY\tINSTALLED\tVERSION\tLATEST\tUPDATE\tPATH")
		_, _ = fmt.Fprintln(w, "----\t--------\t---------\t-------\t------\t------\t----")
	} else {
		_, _ = fmt.Fprintln(w, "TOOL\tCATEGORY\tINSTALLED\tVERSION\tPATH")
		_, _ = fmt.Fprintln(w, "----\t--------\t---------\t-------\t----")
	}

	for _, def := range tools.Registry {
		status, ok := statuses[def.Name]
		if !ok {
			status = mgr.Check(def.Name)
		}
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
		if updates {
			latest := status.LatestVersion
			if latest == "" {
				latest = "-"
			}
			update := "-"
			if status.UpdateAvailable {
				update = "yes"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				def.Name, def.Category, installed, version, latest, update, path)
			continue
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
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

// ToolsInstall installs all missing tools. With force=true every tool is
// removed and reinstalled.
func ToolsInstall(ctx context.Context, force bool) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	var needed []string
	for _, def := range tools.Registry {
		if force || !mgr.IsInstalled(def.Name) {
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
		if force {
			if err := mgr.Delete(name); err != nil {
				fmt.Fprintf(os.Stderr, "Removing %s... FAILED: %v\n", name, err)
				continue
			}
		}
		version, err := mgr.Install(ctx, name, cb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rInstalling %s... FAILED: %v\n", name, err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "\rInstalling %s... done (%s)\n", name, version)
	}

	return nil
}

// ToolsInstallOne installs a single tool by name. With force=true an already
// installed tool is removed and reinstalled (i.e. updated).
func ToolsInstallOne(ctx context.Context, name string, force bool) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	// Validate the tool name exists in registry
	if _, ok := tools.FindByName(name); !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	if mgr.IsInstalled(name) {
		if !force {
			fmt.Printf("Tool %s is already installed (use --force to reinstall/update).\n", name)
			return nil
		}
		if err := mgr.Delete(name); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}

	cb := cliCallbacks()
	version, err := mgr.Install(ctx, name, cb)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\rInstalling %s... FAILED: %v\n", name, err)
		return err
	}
	fmt.Fprintf(os.Stderr, "\rInstalling %s... done (%s)\n", name, version)
	return nil
}
