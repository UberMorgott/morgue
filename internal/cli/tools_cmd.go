package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
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
// reinstalled; Manager.Install stages the new copy and only replaces the old
// one once it is validated, so a failed download keeps the working tool.
func ToolsInstall(ctx context.Context, force bool) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)
	return installMany(ctx, mgr, force, cliCallbacks())
}

// toolInstaller is the part of tools.Manager the bulk installer needs.
type toolInstaller interface {
	IsInstalled(name string) bool
	Install(ctx context.Context, name string, cb *tools.InstallCallbacks) (string, error)
}

// installMany installs every needed registry tool, keeps going past failures
// and returns an aggregate error naming each failed tool (non-zero exit).
func installMany(ctx context.Context, mgr toolInstaller, force bool, cb *tools.InstallCallbacks) error {
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

	fmt.Printf("Installing %d tools...\n", len(needed))
	var failed []string
	for _, name := range needed {
		version, err := mgr.Install(ctx, name, cb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rInstalling %s... FAILED: %v\n", name, err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed = append(failed, name)
			continue
		}
		fmt.Fprintf(os.Stderr, "\rInstalling %s... done (%s)\n", name, version)
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d of %d tools failed to install: %s",
			len(failed), len(needed), strings.Join(failed, ", "))
	}
	return nil
}

// ToolsInstallOne installs a single tool by name. With force=true an already
// installed tool is reinstalled (i.e. updated); the old copy stays in place
// until the new one is downloaded and validated.
func ToolsInstallOne(ctx context.Context, name string, force bool) error {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)

	// Validate the tool name exists in registry
	if _, ok := tools.FindByName(name); !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	if mgr.IsInstalled(name) && !force {
		fmt.Printf("Tool %s is already installed (use --force to reinstall/update).\n", name)
		return nil
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
