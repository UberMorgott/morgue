package odin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeAssetValueBinary(t *testing.T) {
	value, meta, err := DecodeAssetValue(filepath.Join("testdata", "GemComposeAsset.asset"))
	if err != nil {
		t.Fatalf("DecodeAssetValue: %v", err)
	}
	if meta.OdinFormat != "binary" {
		t.Fatalf("OdinFormat = %q, want binary", meta.OdinFormat)
	}
	if meta.Name != "GemComposeAsset" {
		t.Fatalf("meta.Name = %q, want GemComposeAsset", meta.Name)
	}
	if meta.Guid != "f585717f167d85604408057422d30104" {
		t.Fatalf("meta.Guid = %q", meta.Guid)
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
	if got := data["mFurnaceCostIron"]; got != int64(1) {
		t.Fatalf("_data.mFurnaceCostIron = %v (%T), want int64(1)", got, got)
	}
	// Nested dict present, with a payload array under "$items".
	dict, ok := data["mGemSkillLevelScoreDic"].(map[string]any)
	if !ok {
		t.Fatalf("mGemSkillLevelScoreDic is %T, want map", data["mGemSkillLevelScoreDic"])
	}
	if items, ok := dict["$items"].([]any); !ok || len(items) != 6 {
		t.Fatalf("dict $items = %v, want 6 entries", dict["$items"])
	}
	// Nested list present.
	list, ok := data["mComposeRateList"].(map[string]any)
	if !ok {
		t.Fatalf("mComposeRateList is %T, want map", data["mComposeRateList"])
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
