package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// pathPolicy turns event paths into manifest paths: slash-normalized and
// relative to the workspace root, or absolute and external only inside the
// explicitly selected game-data destination.
type pathPolicy struct {
	root     string // workspace root (handle output), cleaned absolute
	rootReal string // root with reparse points resolved; "" = lexical checks only
	gameData string // explicit external destination, cleaned absolute or ""
}

var errPathEscape = errors.New("path escapes workspace")

func newPathPolicy(root, gameData string, checkDisk bool) (pathPolicy, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return pathPolicy{}, err
	}
	p := pathPolicy{root: filepath.Clean(absRoot)}
	if gameData != "" {
		g, err := filepath.Abs(gameData)
		if err != nil {
			return pathPolicy{}, err
		}
		p.gameData = filepath.Clean(g)
	}
	if checkDisk {
		real, err := realPath(p.root)
		if err != nil {
			return pathPolicy{}, fmt.Errorf("resolve workspace root: %w", err)
		}
		p.rootReal = filepath.Clean(real)
	}
	return p, nil
}

// within reports whether p (cleaned, absolute) lies inside base lexically.
func within(base, p string) (string, bool) {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", false
	}
	return rel, true
}

// relative maps p (absolute, or relative to root) to a slash path inside root.
// Empty stays empty (unexecuted target).
func (pp pathPolicy) relative(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	abs := p
	if !filepath.IsAbs(p) {
		if filepath.VolumeName(p) != "" || strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/") {
			return "", fmt.Errorf("%w: %q is not a relative path", errPathEscape, p)
		}
		abs = filepath.Join(pp.root, p)
	}
	rel, ok := within(pp.root, filepath.Clean(abs))
	if !ok {
		return "", fmt.Errorf("%w: %q", errPathEscape, p)
	}
	if err := pp.checkReparse(rel); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// checkReparse resolves every existing component of rel that is a symlink,
// junction or other reparse point and rejects it when it leaves the root.
func (pp pathPolicy) checkReparse(rel string) error {
	if pp.rootReal == "" || rel == "." {
		return nil
	}
	cur := pp.root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // not written yet; nothing to redirect
		}
		if err != nil {
			return fmt.Errorf("inspect %q: %w", cur, err)
		}
		if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			continue
		}
		real, err := realPath(cur)
		if err != nil {
			return fmt.Errorf("%w: cannot resolve reparse point %q: %w", errPathEscape, cur, err)
		}
		if _, ok := within(pp.rootReal, filepath.Clean(real)); !ok {
			return fmt.Errorf("%w: %q resolves outside workspace to %q", errPathEscape, cur, real)
		}
	}
	return nil
}

// artifact normalizes an artifact path. An absolute path outside the root is
// accepted only inside the explicit game-data destination and is then marked
// external; it is recorded as a link and never traversed. A path inside the
// root is always workspace-relative, whatever the producer claimed.
func (pp pathPolicy) artifact(p string, external bool) (string, bool, error) {
	if filepath.IsAbs(p) {
		clean := filepath.Clean(p)
		if _, in := within(pp.root, clean); !in {
			if pp.gameData == "" {
				return "", false, fmt.Errorf("%w: external artifact %q without an explicit game-data destination", errPathEscape, p)
			}
			if _, ok := within(pp.gameData, clean); !ok {
				return "", false, fmt.Errorf("%w: external artifact %q outside game-data destination", errPathEscape, p)
			}
			return filepath.ToSlash(clean), true, nil
		}
	} else if external {
		return "", false, fmt.Errorf("%w: external artifact %q must be absolute", errPathEscape, p)
	}
	rel, err := pp.relative(p)
	if err == nil && rel == "" {
		err = fmt.Errorf("artifact path is empty")
	}
	return rel, false, err
}
