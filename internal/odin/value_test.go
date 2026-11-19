package odin

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeAssetValueBinary(t *testing.T) {
	p := writeOdinAsset(t, t.TempDir(), "Sample", "0123456789abcdef0123456789abcdef", sampleBlob())
	value, meta, err := DecodeAssetValue(p)
	if err != nil {
		t.Fatalf("DecodeAssetValue: %v", err)
	}
	if meta.OdinFormat != "binary" {
		t.Fatalf("OdinFormat = %q, want binary", meta.OdinFormat)
	}
	if meta.Name != "Sample" {
		t.Fatalf("meta.Name = %q, want Sample", meta.Name)
	}
	if meta.Guid != "0123456789abcdef0123456789abcdef" || meta.FileID != 1234567 {
		t.Fatalf("meta.Guid/FileID = %q/%d", meta.Guid, meta.FileID)
	}

	root, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is %T, want map[string]any", value)
	}
	data, ok := root["_data"].(map[string]any)
	if !ok {
		t.Fatalf("_data is %T, want map[string]any", root["_data"])
	}
	// Named scalar field, normalised to int64.
	if got := data["mCost"]; got != int64(1) {
		t.Fatalf("_data.mCost = %v (%T), want int64(1)", got, got)
	}
	if got := data["$type"]; got != "Sample.Asset+Data, Assembly-CSharp" {
		t.Fatalf("_data.$type = %v", got)
	}
	// Nested dict present, with a payload array under "$items".
	dict, ok := data["mScoreDic"].(map[string]any)
	if !ok {
		t.Fatalf("mScoreDic is %T, want map", data["mScoreDic"])
	}
	if items, ok := dict["$items"].([]any); !ok || len(items) != 2 {
		t.Fatalf("dict $items = %v, want 2 entries", dict["$items"])
	}
	// Nested list present.
	list, ok := data["mRateList"].(map[string]any)
	if !ok {
		t.Fatalf("mRateList is %T, want map", data["mRateList"])
	}
	items, ok := list["$items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("list $items = %v, want 2 entries", list["$items"])
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("list[0] is %T, want map", items[0])
	}
	if got := first["mScore"]; got != int64(10) {
		t.Fatalf("list[0].mScore = %v (%T), want int64(10)", got, got)
	}
	// Second element resolves its type via the TypeID cache.
	if second, _ := items[1].(map[string]any); second["$type"] != "Sample.Rate, Assembly-CSharp" || second["mRate"] != 151.13637 {
		t.Fatalf("list[1] = %v", items[1])
	}

	// Scalars, primitive arrays and references, rendered via JSON for brevity.
	for key, want := range map[string]string{
		"mRateList": `{"$items":[{"$type":"Sample.Rate, Assembly-CSharp","mRate":0.35,"mScore":10},{"$type":"Sample.Rate, Assembly-CSharp","mRate":151.13637,"mScore":20}],"$type":"System.Collections.Generic.List` + "`" + `1[[Sample.Rate, Assembly-CSharp]], mscorlib"}`,
		"floats":    `{"$items":[1.5,-0.25],"$type":"System.Single[], mscorlib"}`,
		"doubles":   `{"$items":[2.125],"$type":"System.Double[], mscorlib"}`,
		"ints":      `{"$items":[7,-1],"$type":"System.Int32[], mscorlib"}`,
		"longs":     `{"$items":[1099511627776],"$type":"System.Int64[], mscorlib"}`,
		"bytes":     `{"$items":"dead","$type":"System.Byte[], mscorlib"}`,
		"shorts":    `{"$items":[-2,5],"$type":"#id99"}`,
		"raw8":      `{"$items":[3]}`,
		"raw3":      `{"$items":[1]}`,
		"sb":        `-1`,
		"by":        `200`,
		"sh":        `-3`,
		"us":        `65535`,
		"ui":        `4000000000`,
		"lo":        `-5`,
		"ul":        `18446744073709551615`,
		"do":        `0.1234567891`,
		"de":        `"?"`,
		"ch":        `"Z"`,
		"st":        `"hello"`,
		"gu":        `"?"`,
		"bo":        `true`,
		"nu":        `null`,
		"iref":      `{"$ref":"#5"}`,
		"xref":      `{"$ref":"6"}`,
		"gref":      `{"$ref":"000102030405060708090a0b0c0d0e0f"}`,
		"sref":      `{"$ref":"ref/path"}`,
		"$items": `{"$items":[-128,7,3,4,5,6,7,8.5,9.5,"?","q","u","?",false,null,` +
			`{"$ref":"#"},{"$ref":"2"},{"$ref":"000102030405060708090a0b0c0d0e0f"},{"$ref":"p"}]}`,
	} {
		got, err := json.Marshal(data[key])
		if err != nil || string(got) != want {
			t.Fatalf("_data.%s = %s (%v), want %s", key, got, err, want)
		}
	}
}

func TestDecodeAssetValueYAMLFallback(t *testing.T) {
	// A tab-indented line breaks the YAML parse; the line-scan path must still
	// find the blob and the regex meta extractor must fill Name/Guid.
	p := filepath.Join(t.TempDir(), "Broken.asset")
	body := "MonoBehaviour:\n" +
		"  m_Script: {fileID: 1, guid: 00112233445566778899aabbccddeeff, type: 3}\n" +
		"  m_Name: Broken\n" +
		"\tbad: [\n" +
		"  SerializedBytes: " + hex.EncodeToString(buildBlob()) + "\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	value, meta, err := DecodeAssetValue(p)
	if err != nil {
		t.Fatalf("DecodeAssetValue(fallback): %v", err)
	}
	if meta.OdinFormat != "binary" || meta.Name != "Broken" || meta.Guid != "00112233445566778899aabbccddeeff" {
		t.Fatalf("meta = %+v", meta)
	}
	got, _ := json.Marshal(value)
	if string(got) != `{"root":{"x":7}}` {
		t.Fatalf("value = %s", got)
	}
}

func TestDecodeAssetValueNone(t *testing.T) {
	dir := t.TempDir()
	// Mirror a real Phoenix Point plain-YAML asset: full Unity envelope + real
	// def fields. The envelope keys must be stripped from the returned fields.
	body := "%YAML 1.1\n" +
		"%TAG !u! tag:unity3d.com,2011:\n" +
		"--- !u!114 &11400000\n" +
		"MonoBehaviour:\n" +
		"  m_ObjectHideFlags: 0\n" +
		"  m_CorrespondingSourceObject: {fileID: 0}\n" +
		"  m_PrefabInstance: {fileID: 0}\n" +
		"  m_PrefabAsset: {fileID: 0}\n" +
		"  m_GameObject: {fileID: 0}\n" +
		"  m_Enabled: 1\n" +
		"  m_EditorHideFlags: 0\n" +
		"  m_Script: {fileID: 11500000, guid: abcdef0123456789abcdef0123456789, type: 3}\n" +
		"  m_Name: PlainCamera\n" +
		"  m_EditorClassIdentifier:\n" +
		"  ResourcePath: Defs/PlainCamera\n" +
		"  MaxZoomInLimit: 10\n" +
		"  VerticalAngle: 45\n"
	p := filepath.Join(dir, "PlainCamera.asset")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	value, meta, err := DecodeAssetValue(p)
	if err != nil {
		t.Fatalf("DecodeAssetValue(none): %v", err)
	}
	if meta.OdinFormat != "none" {
		t.Fatalf("OdinFormat = %q, want none", meta.OdinFormat)
	}
	if meta.Name != "PlainCamera" {
		t.Fatalf("meta.Name = %q", meta.Name)
	}
	if meta.Guid != "abcdef0123456789abcdef0123456789" {
		t.Fatalf("meta.Guid = %q", meta.Guid)
	}
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is %T, want map[string]any", value)
	}
	if got := toInt64(m["MaxZoomInLimit"]); got != 10 {
		t.Fatalf("MaxZoomInLimit = %v, want 10", m["MaxZoomInLimit"])
	}
	if m["ResourcePath"] != "Defs/PlainCamera" {
		t.Fatalf("ResourcePath = %v, want kept", m["ResourcePath"])
	}
	// Every Unity envelope key must be gone.
	for _, k := range []string{
		"m_ObjectHideFlags", "m_CorrespondingSourceObject", "m_PrefabInstance",
		"m_PrefabAsset", "m_GameObject", "m_Enabled", "m_EditorHideFlags",
		"m_Script", "m_Name", "m_EditorClassIdentifier",
	} {
		if _, present := m[k]; present {
			t.Fatalf("envelope key %q should be stripped", k)
		}
	}
}
