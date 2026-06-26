package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/UberMorgott/morgue/internal/gamedata"
)

// GameDataOptions holds options for the `morgue gamedata` subcommand.
type GameDataOptions struct {
	ExportDir string
	Out       string
	Inventory string
	Full      string
}

// GameData organizes an existing AssetRipper Unity export into the game-data
// tree, without re-running the export.
func GameData(opts GameDataOptions) error {
	if opts.Out == "" {
		return fmt.Errorf("--out is required")
	}

	exportDir := opts.ExportDir
	if sub := filepath.Join(exportDir, "ExportedProject", "Assets"); isDir(sub) {
		exportDir = sub
	}
	full := opts.Full
	if full == "" {
		full = exportDir
	}

	rep, err := gamedata.Organize(gamedata.Options{
		ExportDir:     exportDir,
		FullExportDir: full,
		OutDir:        opts.Out,
		InventoryCSV:  opts.Inventory,
		Log:           func(s string) { fmt.Fprintln(os.Stderr, s) },
	})
	if err != nil {
		return err
	}

	printGameDataReport(rep, opts.Out)
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func printGameDataReport(rep gamedata.Report, out string) {
	fmt.Printf("GameData tree written to %s\n", out)

	if len(rep.DefCountsByType) > 0 {
		fmt.Println("Defs by type:")
		for _, k := range sortedKeys(rep.DefCountsByType) {
			fmt.Printf("  %-32s %d\n", k, rep.DefCountsByType[k])
		}
	}
	fmt.Printf("Input actions: %d\n", rep.ActionCount)

	if len(rep.LocKeyCounts) > 0 {
		fmt.Println("Localization keys by language:")
		for _, k := range sortedKeys(rep.LocKeyCounts) {
			fmt.Printf("  %-32s %d\n", k, rep.LocKeyCounts[k])
		}
	}
	if len(rep.AssetCountsByType) > 0 {
		fmt.Println("Assets by type:")
		for _, k := range sortedKeys(rep.AssetCountsByType) {
			fmt.Printf("  %-32s %d\n", k, rep.AssetCountsByType[k])
		}
	}
	if len(rep.Failed) > 0 {
		fmt.Printf("Failed (%d):\n", len(rep.Failed))
		for _, f := range rep.Failed {
			fmt.Printf("  %s\n", f)
		}
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
