package gamedata

import (
	"bufio"
	"encoding/csv"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var invOutColumns = []string{"Name", "Type", "Container", "PathID", "SourceBundle"}

// invXMLColumns mirrors invOutColumns with a trailing Size column (the XML
// source reliably carries it); the five spec columns stay first.
var invXMLColumns = []string{"Name", "Type", "Container", "PathID", "SourceBundle", "Size"}

// contentAssetTypes is the allowlist of Unity asset types kept as rows in
// inventory/assets.csv. The scene-graph bulk (GameObject/Transform/MeshRenderer/
// ... — ~13M of 15M rows on Phoenix Point) is intentionally EXCLUDED from the CSV
// so it stays greppable. Report.AssetCountsByType still counts EVERY asset of
// EVERY type, so the manifest's per-type counts remain complete.
var contentAssetTypes = map[string]struct{}{
	"Mesh":                       {},
	"Sprite":                     {},
	"Texture2D":                  {},
	"Texture2DArray":             {},
	"Cubemap":                    {},
	"RenderTexture":              {},
	"Material":                   {},
	"PhysicMaterial":             {},
	"Shader":                     {},
	"ComputeShader":              {},
	"AnimationClip":              {},
	"AnimatorController":         {},
	"AnimatorOverrideController": {},
	"RuntimeAnimatorController":  {},
	"Avatar":                     {},
	"TextAsset":                  {},
	"Font":                       {},
	"AudioClip":                  {},
	"AudioMixer":                 {},
	"VideoClip":                  {},
	"AssetBundle":                {},
	"MonoScript":                 {},
	"MonoBehaviour":              {},
	"ScriptableObject":           {},
}

// isContentType reports whether t is a content asset type kept in assets.csv.
func isContentType(t string) bool {
	_, ok := contentAssetTypes[t]
	return ok
}

// organizeInventory normalizes the AssetStudio inventory into
// inventory/assets.csv. The input format is chosen by extension:
//
//   - *.xml  -> AssetStudioMod `--export-asset-list xml` (streamed; may be
//     multi-GB with millions of <Asset> entries).
//   - else   -> CSV (best-effort header mapping; unknown schema copied through).
func organizeInventory(opts Options, rep *Report) {
	if strings.EqualFold(filepath.Ext(opts.InventoryCSV), ".xml") {
		organizeInventoryXML(opts, rep)
		return
	}
	organizeInventoryCSV(opts, rep)
}

// xmlAsset is one <Asset> element of the AssetStudioMod XML list. <Type> carries
// an id attribute plus the type-name as char data; only the char data is used.
type xmlAsset struct {
	Name      string `xml:"Name"`
	Container string `xml:"Container"`
	Type      string `xml:"Type"`
	PathID    string `xml:"PathID"`
	Source    string `xml:"Source"`
	Size      string `xml:"Size"`
}

// organizeInventoryXML streams the AssetStudioMod XML, writing one CSV row per
// <Asset> as it is decoded so memory stays flat regardless of file size.
func organizeInventoryXML(opts Options, rep *Report) {
	f, err := os.Open(opts.InventoryCSV)
	if err != nil {
		logf(opts, "inventory: "+err.Error())
		rep.Failed = append(rep.Failed, "inventory: "+err.Error())
		return
	}
	defer f.Close()

	w, out, ok := newInventoryWriter(opts, rep)
	if !ok {
		return
	}
	defer out.Close()
	defer w.Flush()
	_ = w.Write(invXMLColumns)

	dec := xml.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	count := 0
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			// Structural error: cannot reliably continue past a broken token.
			rep.Failed = append(rep.Failed, "inventory xml: "+terr.Error())
			break
		}
		se, isStart := tok.(xml.StartElement)
		if !isStart || se.Name.Local != "Asset" {
			continue
		}
		var a xmlAsset
		if e := dec.DecodeElement(&a, &se); e != nil {
			rep.Failed = append(rep.Failed, "inventory asset: "+e.Error())
			continue
		}
		typ := strings.TrimSpace(a.Type)
		if typ == "" {
			typ = "Unknown"
		}
		// Count every asset of every type so manifest counts stay complete...
		rep.AssetCountsByType[typ]++
		// ...but only write content rows so assets.csv stays greppable.
		if isContentType(typ) {
			_ = w.Write([]string{a.Name, a.Type, a.Container, a.PathID, a.Source, a.Size})
		}
		count++
		if count%1000 == 0 {
			w.Flush()
			if werr := w.Error(); werr != nil {
				rep.Failed = append(rep.Failed, "inventory write: "+werr.Error())
				return
			}
		}
	}
	w.Flush()
	if werr := w.Error(); werr != nil {
		rep.Failed = append(rep.Failed, "inventory write: "+werr.Error())
	}
}

// newInventoryWriter creates inventory/assets.csv and its csv.Writer.
func newInventoryWriter(opts Options, rep *Report) (*csv.Writer, *os.File, bool) {
	outPath := filepath.Join(opts.OutDir, "inventory", "assets.csv")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		logf(opts, "inventory: "+err.Error())
		rep.Failed = append(rep.Failed, "inventory: "+err.Error())
		return nil, nil, false
	}
	out, err := os.Create(outPath)
	if err != nil {
		logf(opts, "inventory: "+err.Error())
		rep.Failed = append(rep.Failed, "inventory: "+err.Error())
		return nil, nil, false
	}
	return csv.NewWriter(out), out, true
}

// organizeInventoryCSV normalizes a CSV inventory to assets.csv with columns
// Name,Type,Container,PathID,SourceBundle. It is tolerant of the input schema:
// source columns are matched by best-effort header names; an unrecognized schema
// is copied through verbatim and logged.
func organizeInventoryCSV(opts Options, rep *Report) {
	f, err := os.Open(opts.InventoryCSV)
	if err != nil {
		logf(opts, "inventory: "+err.Error())
		rep.Failed = append(rep.Failed, "inventory: "+err.Error())
		return
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		logf(opts, "inventory parse: "+err.Error())
	}
	if len(rows) == 0 {
		return
	}

	w, out, ok := newInventoryWriter(opts, rep)
	if !ok {
		return
	}
	defer out.Close()
	defer w.Flush()

	idx := mapInventoryColumns(rows[0])
	if len(idx) == 0 {
		// Unknown schema: copy through verbatim so nothing is lost.
		logf(opts, "inventory: unrecognized schema, copying through")
		_ = w.WriteAll(rows)
		return
	}

	_ = w.Write(invOutColumns)
	for _, row := range rows[1:] {
		rec := make([]string, len(invOutColumns))
		for i, c := range invOutColumns {
			if j, ok := idx[c]; ok && j < len(row) {
				rec[i] = row[j]
			}
		}
		typ := rec[1]
		if typ == "" {
			typ = "Unknown"
		}
		rep.AssetCountsByType[typ]++
		if isContentType(typ) {
			_ = w.Write(rec)
		}
	}
}

// mapInventoryColumns maps each output column to a source header index, by
// best-effort case-insensitive substring match. More specific columns are
// resolved first and each source column is consumed at most once.
func mapInventoryColumns(header []string) map[string]int {
	specs := []struct {
		col     string
		needles []string
	}{
		{"PathID", []string{"pathid", "path id", "fileid"}},
		{"SourceBundle", []string{"sourcebundle", "bundle", "source"}},
		{"Container", []string{"container"}},
		{"Type", []string{"type", "class"}},
		{"Name", []string{"name"}},
	}
	used := map[int]bool{}
	idx := map[string]int{}
	for _, sp := range specs {
		for i, h := range header {
			if used[i] {
				continue
			}
			hl := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
			for _, n := range sp.needles {
				if strings.Contains(hl, n) {
					idx[sp.col] = i
					used[i] = true
					break
				}
			}
			if used[i] {
				break
			}
		}
	}
	return idx
}
