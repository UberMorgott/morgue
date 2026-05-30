package scanner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var binaryExtensions = map[string]bool{
	".dll":   true,
	".exe":   true,
	".so":    true,
	".dylib": true,
	".dat":   true,
}

// unrealExtensions are Unreal Engine pak/IoStore container extensions.
// They are discovered separately from the generic binary set so that they
// only ever route into a single Unreal group and never become standalone
// (native/unknown) targets on their own.
var unrealExtensions = map[string]bool{
	".pak":  true,
	".utoc": true,
	".ucas": true,
}

// Scan recursively walks a directory and finds binary files.
func Scan(root string) (ScanResult, error) {
	var result ScanResult
	var pakFiles []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("scanner: skipping %s: %v", path, err)
			result.Skipped = append(result.Skipped, SkippedFile{
				Path:   path,
				Reason: fmt.Sprintf("walk error: %v", err),
			})
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if binaryExtensions[ext] {
			result.Files = append(result.Files, path)
		} else if unrealExtensions[ext] {
			pakFiles = append(pakFiles, path)
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	result.Groups = groupFiles(result.Files, pakFiles)
	return result, nil
}
