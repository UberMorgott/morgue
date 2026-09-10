package odin

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// AssetMeta carries the envelope facts parsed from a Unity .asset wrapper.
//
// OdinFormat is "binary" when the asset holds an Odin SerializedBytes blob, or
// "none" when it is a plain Unity-serialized MonoBehaviour (named YAML fields).
type AssetMeta struct {
	Name       string
	Guid       string
	FileID     int64
	OdinFormat string
}

// DecodeAssetValue parses a Unity .asset file and returns its data as a
// structured Go value (map/slice/scalar) ready for JSON, plus envelope meta.
//
//   - Odin asset (serializationData.SerializedBytes, SerializedFormat 0):
//     OdinFormat="binary", value = the decoded Odin tree.
//   - plain Unity asset (no SerializedBytes): OdinFormat="none", value = the
//     MonoBehaviour YAML mapping.
//
// It never panics: a malformed blob is recovered and returned as an error.
func DecodeAssetValue(path string) (value any, meta AssetMeta, err error) {
	defer func() {
		if r := recover(); r != nil {
			value = nil
			if de, ok := r.(decodeError); ok {
				err = de
			} else {
				err = fmt.Errorf("decode asset %s: %v", path, r)
			}
		}
	}()

	raw, e := os.ReadFile(path) //nolint:gosec // decoding a caller-chosen asset file IS this function's job; the path is a local extraction artifact, not attacker-controlled
	if e != nil {
		return nil, meta, e
	}

	var doc map[string]any
	yerr := yaml.Unmarshal(raw, &doc)
	body := innerBody(doc)

	// Envelope meta (m_Name, m_Script {guid,fileID}).
	meta.Name, _ = body["m_Name"].(string)
	if sc, ok := body["m_Script"].(map[string]any); ok {
		meta.Guid, _ = sc["guid"].(string)
		meta.FileID = toInt64(sc["fileID"])
	}

	// Detect an Odin SerializedBytes blob.
	serBytes := ""
	if sd, ok := body["serializationData"].(map[string]any); ok {
		serBytes, _ = sd["SerializedBytes"].(string)
	}

	if yerr != nil {
		// YAML failed (unusual Unity construct): fall back to the line-scan
		// blob path so an Odin asset still decodes.
		fillMetaRegex(string(raw), &meta)
		if blob, ge := getHex(path); ge == nil {
			v, de := decodeBlobValue(blob)
			if de != nil {
				return nil, meta, de
			}
			meta.OdinFormat = "binary"
			return v, meta, nil
		}
		return nil, meta, fmt.Errorf("parse asset %s: %w", path, yerr)
	}

	if strings.TrimSpace(serBytes) != "" {
		blob, de := hex.DecodeString(strings.TrimSpace(serBytes))
		if de != nil {
			// Hex via YAML scalar can be wrapped/folded; retry via line scan.
			if blob, de = getHex(path); de != nil {
				return nil, meta, de
			}
		}
		v, de := decodeBlobValue(blob)
		if de != nil {
			return nil, meta, de
		}
		meta.OdinFormat = "binary"
		return v, meta, nil
	}

	// Plain Unity-serialized asset: hand back the MonoBehaviour mapping with
	// the Unity envelope keys removed so only real def fields remain.
	meta.OdinFormat = "none"
	return stripEnvelope(body), meta, nil
}

// unityEnvelopeKeys are the boilerplate MonoBehaviour wrapper fields written by
// Unity/AssetRipper; they carry no def data (identity is captured in AssetMeta).
var unityEnvelopeKeys = []string{
	"m_ObjectHideFlags",
	"m_CorrespondingSourceObject",
	"m_PrefabInstance",
	"m_PrefabAsset",
	"m_GameObject",
	"m_Enabled",
	"m_EditorHideFlags",
	"m_Script",
	"m_Name",
	"m_EditorClassIdentifier",
}

// stripEnvelope removes Unity envelope keys from a MonoBehaviour map in place
// and returns it, leaving only the asset's real serialized fields.
func stripEnvelope(body map[string]any) map[string]any {
	for _, k := range unityEnvelopeKeys {
		delete(body, k)
	}
	return body
}

// decodeBlobValue runs the decoder + tree builder inside a recover boundary,
// mirroring decodeBlob but returning a structured value instead of text.
func decodeBlobValue(blob []byte) (val any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if de, ok := r.(decodeError); ok {
				err = de
			} else {
				err = fmt.Errorf("malformed odin blob: %v", r)
			}
		}
	}()
	d := newDecoder(blob)
	d.run()
	return buildTree(d.toks), nil
}

// innerBody returns the inner mapping of a parsed Unity document (the value
// under "MonoBehaviour", or the first mapping value found).
func innerBody(doc map[string]any) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	if mb, ok := doc["MonoBehaviour"].(map[string]any); ok {
		return mb
	}
	for _, v := range doc {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// toInt64 coerces a YAML-decoded numeric (int/int64/uint64/float64/string) to
// int64; non-numeric values yield 0.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case uint64:
		// A Unity fileID is an int64; YAML hands back uint64 only when the
		// scalar exceeds MaxInt64, i.e. a negative id in two's-complement form.
		return int64(n) //nolint:gosec // deliberate two's-complement round-trip of a negative fileID
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	}
	return 0
}

// fillMetaRegex is a best-effort meta extractor for the rare case YAML parsing
// fails; it scans for m_Name and the m_Script guid.
func fillMetaRegex(s string, meta *AssetMeta) {
	if meta.Name == "" {
		if _, rest, found := strings.Cut(s, "m_Name:"); found {
			if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
				meta.Name = strings.TrimSpace(rest[:nl])
			}
		}
	}
	if meta.Guid == "" {
		if _, after, found := strings.Cut(s, "guid:"); found {
			rest := strings.TrimSpace(after)
			end := strings.IndexAny(rest, ",}\r\n ")
			if end < 0 {
				end = len(rest)
			}
			meta.Guid = strings.TrimSpace(rest[:end])
		}
	}
}
