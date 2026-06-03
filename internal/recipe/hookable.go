package recipe

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// boilerplatePrefixes are engine/runtime type-name prefixes (UE + C++ STL) used
// to flag boilerplate classes so game-specific logic is foregrounded. The list
// is generic — it keys on Unreal's and the standard library's naming
// conventions, not on any particular game. Matching is by prefix on the
// outermost class token (so TArray<...> matches "TArray").
var boilerplatePrefixes = []string{
	// Unreal core object model + reflection.
	"UObject", "AActor", "UActorComponent", "UClass", "UStruct", "UFunction",
	"UScriptStruct", "UEnum", "UPackage", "UProperty", "FProperty",
	// Unreal container / string / math primitives (Hungarian F/T prefixes).
	"FName", "FString", "FText", "FArchive", "FVector", "FRotator",
	"FQuat", "FTransform", "FMatrix", "FBox", "FColor", "FGuid",
	"TArray", "TMap", "TSet", "TWeakObjectPtr", "TSharedPtr", "TSharedRef",
	"TUniquePtr", "TFunction", "TOptional", "TSubclassOf",
	// C++ standard library.
	"std::",
}

// isBoilerplateClass reports whether a recovered class name is engine/runtime
// boilerplate (vs game-specific). It matches a maintained generic prefix list
// against the outermost class token (template args stripped).
func isBoilerplateClass(class string) bool {
	// Compare on the leading token before any template '<' so "TArray<int>"
	// matches "TArray". Keep the full string for std:: which contains '::'.
	head := class
	if i := strings.IndexByte(head, '<'); i >= 0 {
		head = head[:i]
	}
	for _, p := range boilerplatePrefixes {
		if strings.HasPrefix(class, p) || strings.HasPrefix(head, p) {
			return true
		}
	}
	return false
}

// hookEntry is one record in indexes/hookable.json: a named function a modder
// can target, with its address and decompiled signature.
type hookEntry struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
}

// writeHookable streams functions.ndjson, overlays the B3 rename map (name_map.csv,
// O(resolved) in RAM), and writes indexes/hookable.json — the set of functions
// that have a real (non-anonymous) name, with addr+name+signature. Anonymous
// FUN_ functions are excluded (no semantic hook target). Returns the count.
// Streaming + memory-safe: only the rename overlay is held in RAM.
func writeHookable(srcDir string) (int, error) {
	fnPath := filepath.Join(srcDir, "functions.ndjson")
	if !fileExists(fnPath) {
		return 0, nil
	}

	// Overlay: address -> resolved name (small; only B3 renames).
	overlay := map[string]string{}
	if nmPath := filepath.Join(srcDir, "indexes", "name_map.csv"); fileExists(nmPath) {
		nm, err := os.Open(nmPath)
		if err != nil {
			return 0, err
		}
		r := csv.NewReader(nm)
		r.FieldsPerRecord = -1
		first := true
		for {
			rec, rerr := r.Read()
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				nm.Close()
				return 0, rerr
			}
			if first {
				first = false
				continue
			}
			if len(rec) >= 3 {
				overlay[rec[0]] = rec[2] // address -> new_name
			}
		}
		nm.Close()
	}

	in, err := os.Open(fnPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(filepath.Join(srcDir, "indexes", "hookable.json"))
	if err != nil {
		return 0, err
	}
	outBuf := bufio.NewWriterSize(out, 64*1024)
	defer func() {
		outBuf.Flush()
		out.Close()
	}()

	// Stream a JSON array: manual "[" / commas / "]" so we never materialize the
	// whole array in RAM.
	if _, err := outBuf.WriteString("[\n"); err != nil {
		return 0, err
	}
	enc := json.NewEncoder(outBuf)
	count := 0

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var fe funcEntry
		if json.Unmarshal(line, &fe) != nil {
			continue
		}
		name := fe.Name
		if nn, ok := overlay[fe.Address]; ok {
			name = nn
		}
		if name == "" || reAnonName.MatchString(name) {
			continue // anonymous → not a hook target
		}
		if count > 0 {
			if _, err := outBuf.WriteString(","); err != nil {
				return count, err
			}
		}
		if err := enc.Encode(&hookEntry{Address: fe.Address, Name: name, Signature: fe.Signature}); err != nil {
			return count, err
		}
		count++
	}
	if scErr := sc.Err(); scErr != nil {
		return count, scErr
	}
	if _, err := outBuf.WriteString("]\n"); err != nil {
		return count, err
	}
	return count, nil
}

// classFlag is one record in indexes/classes.json.
type classFlag struct {
	Name        string `json:"name"`
	Boilerplate bool   `json:"boilerplate"`
}

// writeClassClassification reads the recovered class list from symbols.json and
// writes indexes/classes.json, tagging each class as engine boilerplate or
// game-specific. Nothing is dropped — every recovered class is recorded with a
// flag. Returns (total, boilerplate counts). The class set is small (engine
// classes), so this is trivially memory-safe.
func writeClassClassification(srcDir string) (total, boiler int, err error) {
	symJSON := filepath.Join(srcDir, "symbols.json")
	if !fileExists(symJSON) {
		return 0, 0, nil
	}
	data, err := os.ReadFile(symJSON)
	if err != nil {
		return 0, 0, err
	}
	var sm symbolMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return 0, 0, err
	}

	flags := make([]classFlag, 0, len(sm.Classes))
	for _, c := range sm.Classes {
		b := isBoilerplateClass(c)
		if b {
			boiler++
		}
		flags = append(flags, classFlag{Name: c, Boilerplate: b})
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })

	outData, err := json.MarshalIndent(flags, "", "  ")
	if err != nil {
		return 0, 0, err
	}
	if err := os.WriteFile(filepath.Join(srcDir, "indexes", "classes.json"), outData, 0644); err != nil {
		return 0, 0, err
	}
	return len(sm.Classes), boiler, nil
}
