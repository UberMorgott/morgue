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

// Scan recursively walks a directory and finds binary files.
func Scan(root string) (ScanResult, error) {
	var result ScanResult

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
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	result.Groups = groupFiles(result.Files)
	return result, nil
}
