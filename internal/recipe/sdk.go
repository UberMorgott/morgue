package recipe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// sdk.go: offline SDK class dump generated from a parsed .usmap (see usmap.go).
//
// A .usmap carries the game's full reflected type system — every UClass/UStruct
// with its properties and every UEnum — captured WITHOUT running the game. We
// turn that into a readable "SDK": C++-like class/enum headers plus a machine
// JSON. This replaces the previous honest-but-empty stub that required UE4SS
// runtime injection: the property/field layout needs no runtime at all.
//
// HONEST LIMITATION: a .usmap contains data members (UPROPERTYs) only. It does
// NOT contain UFUNCTION/method signatures, offsets, or sizes — those require
// runtime RTTI/UE4SS. The generated headers therefore declare fields, not
// methods, and README.md states this plainly. For function-level hooks the
// pipeline's Ghidra + name-resolution passes remain the source.

// sdkProperty is one rendered field in the machine JSON.
type sdkProperty struct {
	Name     string `json:"name"`
	Type     string `json:"type"`      // UE property class, e.g. "ArrayProperty"
	CppType  string `json:"cpp_type"`  // readable C++-ish type, e.g. "TArray<FName>"
	ArrayDim int    `json:"array_dim"` // fixed-array dimension (usually 1)
}

// sdkClass is one rendered class/struct in the machine JSON.
type sdkClass struct {
	Name        string        `json:"name"`
	Super       string        `json:"super,omitempty"`
	Boilerplate bool          `json:"boilerplate"` // engine/runtime type (vs game-specific)
	Properties  []sdkProperty `json:"properties"`
}

// sdkEnum is one rendered enum in the machine JSON.
type sdkEnum struct {
	Name   string         `json:"name"`
	Values []sdkEnumValue `json:"values"`
}

type sdkEnumValue struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

// sdkDump is the top-level machine JSON written to sdk/sdk.json.
type sdkDump struct {
	Source       string     `json:"source"`        // path of the source .usmap
	UsmapVersion int        `json:"usmap_version"` // EUsmapVersion
	ClassCount   int        `json:"class_count"`
	EnumCount    int        `json:"enum_count"`
	Classes      []sdkClass `json:"classes"`
	Enums        []sdkEnum  `json:"enums"`
}

// sdkResult summarizes a dump for progress reporting.
type sdkResult struct {
	Classes    int
	Enums      int
	Properties int
	Dir        string
}

// writeSDKDump renders an SDK from parsed usmap data into <outDir>/sdk/. It
// writes sdk.json (machine), classes.hpp + enums.hpp (readable), and README.md
// (provenance + limitations). The input is already fully parsed and clamped by
// the usmap parser, so this is bounded by the (already-capped) class/enum count.
func writeSDKDump(outDir string, m *UsmapData, source string) (sdkResult, error) {
	sdkDir := filepath.Join(outDir, "sdk")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		return sdkResult{}, fmt.Errorf("sdk: mkdir: %w", err)
	}

	// Sort for deterministic, diffable output.
	classes := make([]sdkClass, 0, len(m.Structs))
	propCount := 0
	for _, s := range m.Structs {
		c := sdkClass{
			Name:        s.Name,
			Super:       s.SuperType,
			Boilerplate: isBoilerplateUsmapClass(s.Name),
			Properties:  make([]sdkProperty, 0, len(s.Properties)),
		}
		for _, p := range s.Properties {
			c.Properties = append(c.Properties, sdkProperty{
				Name:     p.Name,
				Type:     p.Type.Type,
				CppType:  cppType(&p.Type),
				ArrayDim: p.ArrayDim,
			})
			propCount++
		}
		classes = append(classes, c)
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i].Name < classes[j].Name })

	enums := make([]sdkEnum, 0, len(m.Enums))
	for _, e := range m.Enums {
		se := sdkEnum{Name: e.Name, Values: make([]sdkEnumValue, 0, len(e.Names))}
		for v, n := range e.Names {
			se.Values = append(se.Values, sdkEnumValue{Name: n, Value: v})
		}
		sort.Slice(se.Values, func(i, j int) bool { return se.Values[i].Value < se.Values[j].Value })
		enums = append(enums, se)
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].Name < enums[j].Name })

	if err := writeSDKJSON(sdkDir, m, source, classes, enums); err != nil {
		return sdkResult{}, err
	}
	if err := writeSDKHeaders(sdkDir, classes); err != nil {
		return sdkResult{}, err
	}
	if err := writeSDKEnums(sdkDir, enums); err != nil {
		return sdkResult{}, err
	}
	if err := writeSDKReadme(sdkDir, source, m.Version, len(classes), len(enums)); err != nil {
		return sdkResult{}, err
	}

	return sdkResult{
		Classes:    len(classes),
		Enums:      len(enums),
		Properties: propCount,
		Dir:        sdkDir,
	}, nil
}

// writeSDKJSON streams the machine JSON so a 17k-class dump never materializes a
// giant single buffer (the class/enum slices are already in RAM, but the encoded
// form is written incrementally).
func writeSDKJSON(sdkDir string, m *UsmapData, source string, classes []sdkClass, enums []sdkEnum) error {
	f, err := os.Create(filepath.Join(sdkDir, "sdk.json"))
	if err != nil {
		return fmt.Errorf("sdk: create sdk.json: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256*1024)
	defer w.Flush()

	dump := sdkDump{
		Source:       filepath.ToSlash(source),
		UsmapVersion: m.Version,
		ClassCount:   len(classes),
		EnumCount:    len(enums),
		Classes:      classes,
		Enums:        enums,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&dump); err != nil {
		return fmt.Errorf("sdk: encode sdk.json: %w", err)
	}
	return nil
}

// writeSDKHeaders renders classes.hpp: one C++-like declaration per class with a
// :public super and field members. Streamed line-by-line.
func writeSDKHeaders(sdkDir string, classes []sdkClass) error {
	f, err := os.Create(filepath.Join(sdkDir, "classes.hpp"))
	if err != nil {
		return fmt.Errorf("sdk: create classes.hpp: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256*1024)
	defer w.Flush()

	fmt.Fprintln(w, "// SDK classes reconstructed offline from .usmap mappings.")
	fmt.Fprintln(w, "// Fields only — .usmap carries UPROPERTYs, not methods/offsets/sizes.")
	fmt.Fprintln(w, "// Engine boilerplate classes are annotated with a // [engine] comment.")
	fmt.Fprintln(w)

	for _, c := range classes {
		tag := ""
		if c.Boilerplate {
			tag = " // [engine]"
		}
		if c.Super != "" {
			fmt.Fprintf(w, "class %s : public %s {%s\n", c.Name, c.Super, tag)
		} else {
			fmt.Fprintf(w, "class %s {%s\n", c.Name, tag)
		}
		for _, p := range c.Properties {
			dim := ""
			if p.ArrayDim > 1 {
				dim = fmt.Sprintf("[%d]", p.ArrayDim)
			}
			fmt.Fprintf(w, "    %s %s%s;\n", p.CppType, p.Name, dim)
		}
		fmt.Fprintln(w, "};")
		fmt.Fprintln(w)
	}
	return nil
}

// writeSDKEnums renders enums.hpp: one `enum class` per enum with value = name.
func writeSDKEnums(sdkDir string, enums []sdkEnum) error {
	f, err := os.Create(filepath.Join(sdkDir, "enums.hpp"))
	if err != nil {
		return fmt.Errorf("sdk: create enums.hpp: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256*1024)
	defer w.Flush()

	fmt.Fprintln(w, "// SDK enums reconstructed offline from .usmap mappings.")
	fmt.Fprintln(w)

	for _, e := range enums {
		fmt.Fprintf(w, "enum class %s {\n", e.Name)
		for _, v := range e.Values {
			fmt.Fprintf(w, "    %s = %d,\n", v.Name, v.Value)
		}
		fmt.Fprintln(w, "};")
		fmt.Fprintln(w)
	}
	return nil
}

// writeSDKReadme records provenance and the honest property-only limitation.
func writeSDKReadme(sdkDir, source string, ver, classes, enums int) error {
	content := fmt.Sprintf(`# Offline SDK Dump

Reconstructed from the game's Unreal Engine **.usmap** mappings file:

- Source: %s
- usmap version: %d (EUsmapVersion)
- Classes/structs: %d
- Enums: %d

## What this is

`+"`sdk.json`"+` is the machine-readable dump (classes with super-type, properties
with UE + C++-like types, enums with explicit values). `+"`classes.hpp`"+` and
`+"`enums.hpp`"+` are human/AI-readable C++-style headers generated from it.

Engine/runtime boilerplate classes are flagged (`+"`boilerplate: true`"+` in JSON,
`+"`// [engine]`"+` in the header) so game-specific types are easy to foreground.

## Honest limitations

A .usmap contains **reflected data members (UPROPERTYs) only**. It does NOT
contain:

- function/method signatures (UFUNCTIONs)
- field offsets or struct sizes
- inline default values

Those require runtime RTTI (e.g. UE4SS injection) or static binary analysis.
For function-level information, see the pipeline's Ghidra decompilation and
name-resolution outputs. This SDK is generated fully offline with no runtime.
`, filepath.ToSlash(source), ver, classes, enums)

	if err := os.WriteFile(filepath.Join(sdkDir, "README.md"), []byte(content), 0644); err != nil {
		return fmt.Errorf("sdk: write README: %w", err)
	}
	return nil
}

// usmapCompressionName renders an EUsmapCompressionMethod as a label.
func usmapCompressionName(m int) string {
	switch m {
	case usmapNone:
		return "None"
	case usmapOodle:
		return "Oodle"
	case usmapBrotli:
		return "Brotli"
	case usmapZStandard:
		return "ZStandard"
	default:
		return fmt.Sprintf("Unknown(%d)", m)
	}
}

// buildUsmapResolution derives the offline name-resolution enrichment from a
// parsed usmap: totals, the join against Ghidra symbol classes (symbols.json),
// and the join against parsed asset name tables (assets_index.json). All inputs
// are small summaries (symbols.json/assets_index.json are bounded), so this is
// memory-safe. Joins are name-based and prefix-insensitive (usmap names lack the
// U/A/F prefix that symbol/asset names may carry).
func buildUsmapResolution(m *UsmapData, srcDir, outDir string) *usmapResolution {
	ur := &usmapResolution{
		Version:           m.Version,
		CompressionMethod: usmapCompressionName(m.CompressionMethod),
		Classes:           len(m.Structs),
		Enums:             len(m.Enums),
	}
	for _, s := range m.Structs {
		ur.Properties += len(s.Properties)
	}

	// Exact set of usmap class names (usmap names carry NO U/A/F type prefix).
	usmapSet := make(map[string]bool, len(m.Structs))
	for _, s := range m.Structs {
		usmapSet[s.Name] = true
	}

	// matchUsmap returns the usmap class name an external (symbol/asset) name
	// refers to, trying the exact name first and then the name with a single
	// leading U/A/F type-prefix stripped (e.g. "AMyPawn" -> "MyPawn"). It never
	// strips a prefix from the usmap side, so it cannot create the "Apple"->"pple"
	// false collisions a blind normalize would: only the EXTERNAL prefixed form
	// is reduced, and only when the unprefixed form is an actual usmap class.
	matchUsmap := func(external string) (string, bool) {
		if usmapSet[external] {
			return external, true
		}
		if stripped := stripUEPrefix(external); stripped != external && usmapSet[stripped] {
			return stripped, true
		}
		return "", false
	}

	// --- Join against Ghidra symbol classes ---
	if symJSON := filepath.Join(srcDir, "symbols.json"); fileExists(symJSON) {
		if data, err := os.ReadFile(symJSON); err == nil {
			var sm symbolMap
			if json.Unmarshal(data, &sm) == nil {
				ur.SymbolClassesTotal = len(sm.Classes)
				matched := map[string]bool{}
				for _, c := range sm.Classes {
					if name, ok := matchUsmap(c); ok {
						matched[name] = true
					}
				}
				ur.MatchedSymbolClasses = len(matched)
				// Sample a few matched, non-engine classes for readability.
				for name := range matched {
					if !isBoilerplateUsmapClass(name) {
						ur.SampleGameClasses = append(ur.SampleGameClasses, name)
						if len(ur.SampleGameClasses) >= 20 {
							break
						}
					}
				}
				sort.Strings(ur.SampleGameClasses)
			}
		}
	}

	// --- Join against parsed asset name tables ---
	if aiPath := filepath.Join(outDir, "assets_index.json"); fileExists(aiPath) {
		if data, err := os.ReadFile(aiPath); err == nil {
			var ai assetsIndex
			if json.Unmarshal(data, &ai) == nil {
				matched := map[string]bool{}
				for _, a := range ai.Sample {
					for _, n := range a.Names {
						if name, ok := matchUsmap(n); ok {
							matched[name] = true
						}
					}
				}
				ur.MatchedAssetNames = len(matched)
			}
		}
	}

	return ur
}

// stripUEPrefix removes a single leading U/A/F when followed by an uppercase
// letter — the Unreal type-prefix convention (AActor, UObject, FVector). It is
// only ever applied to the PREFIXED (external) side of a join, gated by an exact
// usmap-set hit, so it cannot misfire on names like "Apple". Returns the input
// unchanged when no such prefix is present.
func stripUEPrefix(name string) string {
	if len(name) >= 2 {
		switch name[0] {
		case 'U', 'A', 'F':
			if name[1] >= 'A' && name[1] <= 'Z' {
				return name[1:]
			}
		}
	}
	return name
}

// sdkFallbackFromSymbols writes a class-name-only SDK from <outDir>/src/symbols.json
// when no .usmap is available. It records the recovered class names (flagged
// engine/game) but NO properties — symbols.json carries names only. Returns the
// class count (0 if symbols.json is absent/empty). The README states the
// limitation. symbols.json is a small summary (not the multi-GB ndjson), so this
// is memory-safe.
func sdkFallbackFromSymbols(outDir string, log func(string)) int {
	symJSON := filepath.Join(outDir, "src", "symbols.json")
	if !fileExists(symJSON) {
		return 0
	}
	data, err := os.ReadFile(symJSON)
	if err != nil {
		if log != nil {
			log(fmt.Sprintf("SDK fallback: read symbols.json: %v", err))
		}
		return 0
	}
	var sm symbolMap
	if err := json.Unmarshal(data, &sm); err != nil {
		if log != nil {
			log(fmt.Sprintf("SDK fallback: parse symbols.json: %v", err))
		}
		return 0
	}
	if len(sm.Classes) == 0 {
		return 0
	}

	classes := make([]sdkClass, 0, len(sm.Classes))
	for _, name := range sm.Classes {
		classes = append(classes, sdkClass{
			Name:        name,
			Boilerplate: isBoilerplateClass(name), // symbols.json names ARE prefixed
			Properties:  []sdkProperty{},
		})
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i].Name < classes[j].Name })

	sdkDir := filepath.Join(outDir, "sdk")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		if log != nil {
			log(fmt.Sprintf("SDK fallback: mkdir: %v", err))
		}
		return 0
	}
	dump := sdkDump{
		Source:     "symbols.json (no .usmap available — class names only)",
		ClassCount: len(classes),
		Classes:    classes,
		Enums:      []sdkEnum{},
	}
	out, err := json.MarshalIndent(&dump, "", "  ")
	if err != nil {
		return 0
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "sdk.json"), out, 0644); err != nil {
		return 0
	}
	readme := fmt.Sprintf(`# Offline SDK Dump (fallback)

No .usmap mappings file was found, so this dump lists recovered **class names
only** (from Ghidra symbols.json) with NO property/field layout and NO enums.

- Classes: %d

To get full class/struct/enum + property layout, provide a .usmap mappings file
(e.g. dumped with UE4SS / UnrealMappingsDumper) next to the game and re-run.
`, len(classes))
	_ = os.WriteFile(filepath.Join(sdkDir, "README.md"), []byte(readme), 0644)
	return len(classes)
}

// isBoilerplateUsmapClass adapts the prefixed engine-boilerplate detector to
// .usmap names, which are stored WITHOUT the leading U/A/F type prefix (e.g.
// "Actor", "Object", "ActorComponent" rather than "AActor"/"UObject"). It tests
// the bare name and each prefixed candidate against isBoilerplateClass so a
// usmap class is flagged exactly when its conventional engine form would be.
func isBoilerplateUsmapClass(name string) bool {
	if isBoilerplateClass(name) {
		return true
	}
	for _, pfx := range []string{"U", "A", "F"} {
		if isBoilerplateClass(pfx + name) {
			return true
		}
	}
	return false
}

// cppType renders a UsmapPropertyType as a readable C++-ish type string. Scalar
// UE property classes map to their C++ primitive; containers recurse.
func cppType(t *UsmapPropertyType) string {
	if t == nil {
		return "void"
	}
	switch t.Type {
	case "BoolProperty":
		return "bool"
	case "ByteProperty":
		return "uint8"
	case "Int8Property":
		return "int8"
	case "Int16Property":
		return "int16"
	case "IntProperty":
		return "int32"
	case "Int64Property":
		return "int64"
	case "UInt16Property":
		return "uint16"
	case "UInt32Property":
		return "uint32"
	case "UInt64Property":
		return "uint64"
	case "FloatProperty":
		return "float"
	case "DoubleProperty":
		return "double"
	case "NameProperty":
		return "FName"
	case "StrProperty", "Utf8StrProperty", "AnsiStrProperty":
		return "FString"
	case "TextProperty":
		return "FText"
	case "ObjectProperty", "ClassProperty":
		return "UObject*"
	case "WeakObjectProperty":
		return "TWeakObjectPtr<UObject>"
	case "LazyObjectProperty":
		return "TLazyObjectPtr<UObject>"
	case "SoftObjectProperty", "AssetObjectProperty":
		return "TSoftObjectPtr<UObject>"
	case "SoftClassProperty":
		return "TSoftClassPtr<UObject>"
	case "InterfaceProperty":
		return "TScriptInterface<>"
	case "DelegateProperty":
		return "FScriptDelegate"
	case "MulticastDelegateProperty", "MulticastInlineDelegateProperty":
		return "FMulticastScriptDelegate"
	case "FieldPathProperty":
		return "FFieldPath"
	case "StructProperty":
		if t.StructType != "" {
			return "F" + t.StructType
		}
		return "FStruct"
	case "EnumProperty":
		if t.EnumName != "" {
			return t.EnumName
		}
		return "uint8"
	case "ArrayProperty":
		return "TArray<" + cppType(t.Inner) + ">"
	case "SetProperty":
		return "TSet<" + cppType(t.Inner) + ">"
	case "OptionalProperty":
		return "TOptional<" + cppType(t.Inner) + ">"
	case "MapProperty":
		return "TMap<" + cppType(t.Inner) + ", " + cppType(t.Value) + ">"
	default:
		// Unknown(NNN) and any future type render as the raw tag.
		return t.Type
	}
}
