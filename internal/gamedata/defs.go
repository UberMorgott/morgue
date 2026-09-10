package gamedata

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/UberMorgott/morgue/internal/odin"
)

// defResult is the aggregate output of the defs pass.
type defResult struct {
	Counts    map[string]int    // TypeName -> count
	Index     map[string]string // asset guid OR pathID -> DefName (for ref resolution)
	Failed    []string
	InputMaps []map[string]any // fields of any def whose TypeName == "InputMapDef"
	I2Sources []map[string]any // fields of any I2 localization source (has mSource.mTerms)
}

// defRecord is the on-disk JSON shape of one def file.
type defRecord struct {
	TypeName   string `json:"typeName"`
	DefName    string `json:"defName"`
	Guid       string `json:"guid"`
	FileID     int64  `json:"fileID"`
	OdinFormat string `json:"odinFormat"`
	Fields     any    `json:"fields"`
}

// organizeDefs walks ExportDir for *.asset, decodes each, and writes
// defs/<TypeName>/<DefName>.json. Per-file failures are recorded, not fatal.
func organizeDefs(opts Options, scriptMap map[string]string) defResult {
	res := defResult{Counts: map[string]int{}, Index: map[string]string{}}
	if opts.ExportDir == "" {
		return res
	}
	var assets []string
	_ = filepath.WalkDir(opts.ExportDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped so the rest of the export still decodes
		}
		// Only MonoBehaviour (Unity class 114) assets are ScriptableObject defs.
		// Skip binary assets (Mesh, Texture2DArray, Sprite, …) by peeking the
		// document header: their bodies can be hundreds of MiB and would OOM
		// the YAML parser if read in full.
		if strings.EqualFold(filepath.Ext(d.Name()), ".asset") && isMonoBehaviourAsset(path) {
			assets = append(assets, path)
		}
		return nil
	})

	total := len(assets)
	seen := map[string]int{} // dedupe colliding <Type>/<Name> outputs
	for i, p := range assets {
		progress(opts, i+1, total, "defs")
		rec, assetGuid, anchorID, err := decodeDef(p, scriptMap)
		if err != nil {
			res.Failed = append(res.Failed, relPath(opts.ExportDir, p)+": "+err.Error())
			continue
		}

		typeDir := sanitizeName(rec.TypeName)
		fname := sanitizeName(rec.DefName)
		key := typeDir + "/" + fname
		if n := seen[key]; n > 0 {
			fname = fmt.Sprintf("%s_%d", fname, n+1)
		}
		seen[key]++

		outPath := filepath.Join(opts.OutDir, "defs", typeDir, fname+".json")
		if err := writeJSON(outPath, rec); err != nil {
			res.Failed = append(res.Failed, relPath(opts.ExportDir, p)+": "+err.Error())
			continue
		}
		res.Counts[rec.TypeName]++

		if assetGuid != "" {
			res.Index[assetGuid] = rec.DefName
		}
		if anchorID != 0 {
			res.Index[strconv.FormatInt(anchorID, 10)] = rec.DefName
		}
		if fm := asMap(rec.Fields); fm != nil {
			if rec.TypeName == "InputMapDef" {
				res.InputMaps = append(res.InputMaps, fm)
			}
			if isI2Source(fm) {
				res.I2Sources = append(res.I2Sources, fm)
			}
		}
	}
	return res
}

// isI2Source reports whether a def's fields look like an I2 Localization source
// (mSource.mTerms present), regardless of its resolved TypeName.
func isI2Source(fields map[string]any) bool {
	src := asMap(fields["mSource"])
	if src == nil {
		return false
	}
	_, ok := src["mTerms"]
	return ok
}

// decodeDef decodes one .asset into a defRecord plus its asset guid (sibling
// .meta) and anchor pathID, recover-wrapped so a malformed file returns an
// error rather than panicking.
func decodeDef(path string, scriptMap map[string]string) (rec defRecord, assetGuid string, anchorID int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic decoding def: %v", r)
		}
	}()

	val, meta, e := odin.DecodeAssetValue(path)
	if e != nil {
		return rec, "", 0, e
	}
	typeName := scriptMap[meta.Guid]
	if typeName == "" {
		typeName = "UnknownType"
	}
	defName := meta.Name
	if defName == "" {
		defName = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	rec = defRecord{
		TypeName:   typeName,
		DefName:    defName,
		Guid:       meta.Guid,
		FileID:     meta.FileID,
		OdinFormat: meta.OdinFormat,
		Fields:     val,
	}
	return rec, readAssetGuid(path), readAnchorID(path), nil
}

// readAssetGuid returns the asset's own guid from its sibling "<asset>.meta".
func readAssetGuid(assetPath string) string {
	return parseGuidFromMeta(assetPath + ".meta")
}

var (
	anchorRe = regexp.MustCompile(`&(\d+)`)
	classRe  = regexp.MustCompile(`^--- !u!(\d+)`)
)

// isMonoBehaviourAsset reports whether the first Unity document in the file is a
// MonoBehaviour (class 114) — i.e. a ScriptableObject def. It reads only the
// header line, so multi-hundred-MiB binary assets (Mesh, Texture2DArray, …) are
// classified without loading their bodies into memory.
func isMonoBehaviourAsset(path string) bool {
	f, err := os.Open(path) //nolint:gosec // path comes from WalkDir over the caller's local export dir
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "--- ") {
			m := classRe.FindStringSubmatch(line)
			return len(m) > 1 && m[1] == "114"
		}
	}
	return false
}

// readAnchorID returns the Unity fileID (pathID) from the first "--- !u!N &M"
// document header line.
func readAnchorID(assetPath string) int64 {
	f, err := os.Open(assetPath) //nolint:gosec // assetPath is a local export file already accepted by the defs walk
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "--- ") {
			if m := anchorRe.FindStringSubmatch(line); m != nil {
				id, _ := strconv.ParseInt(m[1], 10, 64)
				return id
			}
		}
	}
	return 0
}
