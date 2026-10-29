package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const inventoryScope = "payload-inventory-v2"

// inventoryHash covers executable plus DLLs/JARs/plugins, never just a launcher.
// Only known mutable directory names, root .version and Ghidra *.sla.lock
// sentinels are excluded (v2 added the sentinels).
func inventoryHash(ctx context.Context, root string) (string, error) {
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() && rel != "." {
			switch strings.ToLower(d.Name()) {
			case ".cache", "cache", "logs", "temp", "tmp":
				return filepath.SkipDir
			}
		}
		if rel == ".version" {
			return nil
		}
		// Ghidra creates <lang>.sla.lock sentinels next to SLEIGH files on
		// launch; runtime state, not payload.
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".sla.lock") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || reparseEntry(info) {
			return fmt.Errorf("payload reparse point is not supported: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular payload file: %s", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		buf := make([]byte, 128*1024)
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			var n int
			n, err = f.Read(buf)
			if n > 0 {
				_, _ = h.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		if err == io.EOF {
			err = nil
		}
		cerr := f.Close()
		if err != nil {
			return err
		}
		if cerr != nil {
			return cerr
		}
		after, err := os.Stat(path)
		if err != nil {
			return err
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("payload changed while hashing: %s", path)
		}
		entries = append(entries, filepath.ToSlash(rel)+"\x00"+hex.EncodeToString(h.Sum(nil))+"\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("empty payload: %s", root)
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, entry := range entries {
		_, _ = io.WriteString(h, entry)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func observedVersion(version string) string {
	if version == "" || strings.EqualFold(version, "latest") || version == systemVersion || version == "–" {
		return "unknown"
	}
	return version
}

func sourceFor(tool ToolDef) string {
	switch tool.Method {
	case MethodGitHubRelease:
		return "github"
	case MethodNuGet:
		return "nuget"
	case MethodDotnetTool:
		return "dotnet"
	case MethodDirectURL:
		return "direct"
	}
	return "unknown"
}

func identityAt(ctx context.Context, tool ToolDef, root, version string, managed bool) (ToolIdentity, error) {
	hash, err := inventoryHash(ctx, root)
	if err != nil {
		return ToolIdentity{}, err
	}
	source := sourceFor(tool)
	if !managed {
		source = "system"
	}
	return ToolIdentity{Name: tool.Name, Version: observedVersion(version), Source: source, PayloadSHA256: hash, Managed: managed, HashScope: inventoryScope}, nil
}
