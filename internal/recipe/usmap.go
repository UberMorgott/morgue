package recipe

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/klauspost/compress/zstd"
)

// usmap.go: an offline parser for Unreal Engine .usmap "mappings" files.
//
// A .usmap encodes the reflected type system of a UE game: the name table, every
// UEnum (with its values), and every UStruct/UClass schema with its properties
// and types. It is produced by tools like UE4SS/UnrealMappingsDumper and is the
// single richest source of class/property layout available WITHOUT running the
// game. We parse it fully offline so downstream passes (SDK dump, name
// resolution enrichment) can emit AI-readable type information.
//
// Format reference: CUE4Parse/MappingsProvider/Usmap (UsmapParser.cs,
// UsmapProperties.cs, EPropertyType.cs). Verified byte-for-byte against a real
// ue4ss Mappings.usmap (version 4, Zstd-compressed). Header layout:
//
//	uint16 magic = 0x30C4
//	uint8  version
//	if version >= 1 (PackageVersioning): uint8 bHasVersioning;
//	    if true: int32 UE4ver, int32 UE5ver, FCustomVersionContainer, uint32 NetCL
//	uint8  compressionMethod (0=None,1=Oodle,2=Brotli,3=Zstd)
//	uint32 compressedSize, uint32 decompressedSize, [compressed body]
//
// Decompressed body:
//
//	uint32 nameCount; per name: len(uint8, or uint16 if version>=2 LongFName) + ASCII bytes
//	uint32 enumCount;  per enum: name(int32 idx), entryCount(uint8, or uint16 if version>=3),
//	    per entry: if version>=4 ExplicitEnumValues: uint64 value + int32 name; else int32 name
//	uint32 structCount; per struct: name(int32), super(int32, -1=none),
//	    uint16 propertyCount, uint16 serializablePropertyCount,
//	    per serializable prop: uint16 index, uint8 arrayDim, int32 name, PropertyType
//	PropertyType: uint8 EPropertyType + recursion (Enum: inner+name; Struct: name;
//	    Array/Set/Optional: inner; Map: inner+value)
//
// MEMORY SAFETY: every count and length in this file comes from untrusted bytes.
// Each is clamped against a hard maximum BEFORE any make()/loop, and the
// decompressed size is clamped before allocation. The Zstd decoder is capped via
// WithDecoderMaxMemory. A corrupt file yields an error, never a giant alloc.

// usmapMagic is the file magic 0x30C4 (on disk little-endian: C4 30).
const usmapMagic uint16 = 0x30C4

// usmap version milestones (EUsmapVersion). Each gates a wire-format change.
const (
	usmapVerInitial        = 0 // base format
	usmapVerPackageVersion = 1 // adds optional package versioning block
	usmapVerLongFName      = 2 // name lengths become uint16 (were uint8)
	usmapVerLargeEnums     = 3 // enum entry counts become uint16 (were uint8)
	usmapVerExplicitEnums  = 4 // enum entries carry explicit uint64 values
	usmapVerLatest         = usmapVerExplicitEnums
)

// usmap compression methods (EUsmapCompressionMethod).
const (
	usmapNone      = 0
	usmapOodle     = 1
	usmapBrotli    = 2
	usmapZStandard = 3
)

// EPropertyType values (from CUE4Parse EPropertyType.cs). Used to label property
// types and to drive recursion for container/enum/struct properties.
const (
	ptByte                    = 0
	ptBool                    = 1
	ptInt                     = 2
	ptFloat                   = 3
	ptObject                  = 4
	ptName                    = 5
	ptDelegate                = 6
	ptDouble                  = 7
	ptArray                   = 8
	ptStruct                  = 9
	ptStr                     = 10
	ptText                    = 11
	ptInterface               = 12
	ptMulticastDelegate       = 13
	ptWeakObject              = 14
	ptLazyObject              = 15
	ptAssetObject             = 16
	ptSoftObject              = 17
	ptUInt64                  = 18
	ptUInt32                  = 19
	ptUInt16                  = 20
	ptInt64                   = 21
	ptInt16                   = 22
	ptInt8                    = 23
	ptMap                     = 24
	ptSet                     = 25
	ptEnum                    = 26
	ptFieldPath               = 27
	ptOptional                = 28
	ptUtf8Str                 = 29
	ptAnsiStr                 = 30
	ptClass                   = 31
	ptMulticastInlineDelegate = 32
	ptSoftClass               = 33
	ptVerseString             = 34
	ptVerseDynamic            = 35
	ptVerseFunction           = 36
)

// propertyTypeNames maps EPropertyType -> the canonical UE property class name.
// Anything outside the table renders as "Unknown(<n>)".
var propertyTypeNames = map[int]string{
	ptByte: "ByteProperty", ptBool: "BoolProperty", ptInt: "IntProperty",
	ptFloat: "FloatProperty", ptObject: "ObjectProperty", ptName: "NameProperty",
	ptDelegate: "DelegateProperty", ptDouble: "DoubleProperty", ptArray: "ArrayProperty",
	ptStruct: "StructProperty", ptStr: "StrProperty", ptText: "TextProperty",
	ptInterface: "InterfaceProperty", ptMulticastDelegate: "MulticastDelegateProperty",
	ptWeakObject: "WeakObjectProperty", ptLazyObject: "LazyObjectProperty",
	ptAssetObject: "AssetObjectProperty", ptSoftObject: "SoftObjectProperty",
	ptUInt64: "UInt64Property", ptUInt32: "UInt32Property", ptUInt16: "UInt16Property",
	ptInt64: "Int64Property", ptInt16: "Int16Property", ptInt8: "Int8Property",
	ptMap: "MapProperty", ptSet: "SetProperty", ptEnum: "EnumProperty",
	ptFieldPath: "FieldPathProperty", ptOptional: "OptionalProperty",
	ptUtf8Str: "Utf8StrProperty", ptAnsiStr: "AnsiStrProperty", ptClass: "ClassProperty",
	ptMulticastInlineDelegate: "MulticastInlineDelegateProperty",
	ptSoftClass:               "SoftClassProperty", ptVerseString: "VerseStringProperty",
	ptVerseDynamic: "VerseDynamicProperty", ptVerseFunction: "VerseFunctionProperty",
}

// Hard clamps. UE games top out in the low hundreds of thousands of names and
// tens of thousands of classes; these caps are generous yet bound any corrupt
// count to a sane allocation rather than the multi-GB OOM that froze this
// machine once. (See team rule: clamp before any make() sized by file data.)
const (
	usmapMaxDecompSize = 512 << 20 // 512 MiB decompressed body ceiling
	usmapMaxNames      = 5_000_000
	usmapMaxEnums      = 2_000_000
	usmapMaxStructs    = 2_000_000
	usmapMaxEnumVals   = 1_000_000 // per single enum
	usmapMaxProps      = 1_000_000 // per single struct
	usmapMaxFileSize   = 256 << 20 // refuse usmap files larger than this on disk
	usmapMaxStringLen  = 1 << 20   // 1 MiB single name (already bounded by uint16)
	usmapMaxNestDepth  = 32        // property-type recursion guard
	zstdMaxDecoderMem  = 1 << 30   // 1 GiB Zstd window/table ceiling
)

// ErrUsmapOodle is returned when a usmap is Oodle-compressed: Oodle is a
// proprietary codec with no offline Go decoder, so we honestly cannot decode it.
var ErrUsmapOodle = fmt.Errorf("usmap: Oodle-compressed mappings cannot be decoded offline (proprietary codec)")

// UsmapPropertyType is one (possibly nested) property type. For containers the
// Inner/Value fields recurse; for StructProperty StructType names the struct;
// for EnumProperty EnumName names the enum and Inner is the underlying type.
type UsmapPropertyType struct {
	Type       string             `json:"type"`                  // e.g. "ArrayProperty"
	StructType string             `json:"struct_type,omitempty"` // StructProperty target
	EnumName   string             `json:"enum_name,omitempty"`   // EnumProperty target
	Inner      *UsmapPropertyType `json:"inner,omitempty"`       // array/set/optional/map-key/enum-underlying
	Value      *UsmapPropertyType `json:"value,omitempty"`       // map value type
}

// String renders a property type compactly, e.g. "TArray<ObjectProperty>".
func (t *UsmapPropertyType) String() string {
	if t == nil {
		return ""
	}
	switch t.Type {
	case "ArrayProperty":
		return "TArray<" + t.Inner.String() + ">"
	case "SetProperty":
		return "TSet<" + t.Inner.String() + ">"
	case "OptionalProperty":
		return "TOptional<" + t.Inner.String() + ">"
	case "MapProperty":
		return "TMap<" + t.Inner.String() + ", " + t.Value.String() + ">"
	case "StructProperty":
		if t.StructType != "" {
			return "F" + t.StructType
		}
		return "StructProperty"
	case "EnumProperty":
		if t.EnumName != "" {
			return t.EnumName
		}
		return "EnumProperty"
	default:
		return t.Type
	}
}

// UsmapProperty is one reflected property of a struct/class.
type UsmapProperty struct {
	Index    int               `json:"index"`     // schema slot index
	Name     string            `json:"name"`      // property name
	ArrayDim int               `json:"array_dim"` // fixed-array dimension (usually 1)
	Type     UsmapPropertyType `json:"type"`
}

// UsmapStruct is one reflected UStruct/UClass schema.
type UsmapStruct struct {
	Name       string          `json:"name"`
	SuperType  string          `json:"super,omitempty"` // parent class/struct, "" if none
	Properties []UsmapProperty `json:"properties"`
}

// UsmapEnum is one reflected UEnum: ordered value->name pairs.
type UsmapEnum struct {
	Name  string            `json:"name"`
	Names map[uint64]string `json:"names"`
}

// UsmapData is the fully-parsed contents of a .usmap file.
type UsmapData struct {
	Version           int           `json:"version"`
	CompressionMethod int           `json:"compression_method"`
	Names             []string      `json:"-"` // raw name LUT (large; not serialized)
	Enums             []UsmapEnum   `json:"enums"`
	Structs           []UsmapStruct `json:"structs"`
}

// ParseUsmap reads and fully parses a .usmap file from disk. It is the offline
// entry point. Returns ErrUsmapOodle for Oodle-compressed files.
func ParseUsmap(path string) (*UsmapData, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("usmap: stat: %w", err)
	}
	if fi.Size() > usmapMaxFileSize {
		return nil, fmt.Errorf("usmap: file too large (%d bytes, max %d)", fi.Size(), usmapMaxFileSize)
	}
	// File is small (< 256 MiB by the check above) so a single read is safe and
	// avoids streaming complexity for the header parse.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("usmap: read: %w", err)
	}
	return parseUsmapBytes(raw)
}

// parseUsmapBytes parses an in-memory .usmap image (header + compressed body).
func parseUsmapBytes(raw []byte) (*UsmapData, error) {
	r := &byteReader{data: raw}

	magic, err := r.uint16()
	if err != nil {
		return nil, fmt.Errorf("usmap: read magic: %w", err)
	}
	if magic != usmapMagic {
		return nil, fmt.Errorf("usmap: bad magic 0x%04X (want 0x%04X)", magic, usmapMagic)
	}

	version, err := r.uint8()
	if err != nil {
		return nil, fmt.Errorf("usmap: read version: %w", err)
	}
	if int(version) > usmapVerLatest {
		return nil, fmt.Errorf("usmap: unsupported version %d (latest supported %d)", version, usmapVerLatest)
	}

	out := &UsmapData{Version: int(version)}

	// Optional package-versioning block. NOTE: UE FArchive serializes bool as a
	// 4-byte uint32 (0/1), so bHasVersioning is read as uint32, not a single
	// byte. Verified against the real ue4ss Mappings.usmap (version 4): the
	// compression-method byte sits at offset 7, i.e. after a 4-byte false flag.
	if int(version) >= usmapVerPackageVersion {
		hasVersioning, err := r.uint32()
		if err != nil {
			return nil, fmt.Errorf("usmap: read versioning flag: %w", err)
		}
		if hasVersioning != 0 {
			if err := skipVersioningBlock(r); err != nil {
				return nil, err
			}
		}
	}

	compMethod, err := r.uint8()
	if err != nil {
		return nil, fmt.Errorf("usmap: read compression method: %w", err)
	}
	out.CompressionMethod = int(compMethod)

	compSize, err := r.uint32()
	if err != nil {
		return nil, fmt.Errorf("usmap: read comp size: %w", err)
	}
	decompSize, err := r.uint32()
	if err != nil {
		return nil, fmt.Errorf("usmap: read decomp size: %w", err)
	}

	// MEMORY SAFETY: clamp decompressed size before any allocation.
	if decompSize > usmapMaxDecompSize {
		return nil, fmt.Errorf("usmap: decompressed size %d exceeds cap %d", decompSize, usmapMaxDecompSize)
	}
	// The compressed body must actually be present in the buffer.
	if int(compSize) > r.remaining() {
		return nil, fmt.Errorf("usmap: comp size %d exceeds remaining %d", compSize, r.remaining())
	}
	comp := r.data[r.pos : r.pos+int(compSize)]

	body, err := decompressUsmap(int(compMethod), comp, int(decompSize))
	if err != nil {
		return nil, err
	}

	if err := parseUsmapBody(out, body); err != nil {
		return nil, err
	}
	return out, nil
}

// skipVersioningBlock consumes the optional FPackageFileVersion + custom version
// container + NetCL. We don't use these fields, but must skip them precisely to
// stay aligned. Layout: int32 UE4ver, int32 UE5ver, FCustomVersionContainer
// (int32 count + count*{16-byte GUID, int32 version}), uint32 NetCL.
func skipVersioningBlock(r *byteReader) error {
	if err := r.skip(8); err != nil { // UE4ver + UE5ver
		return fmt.Errorf("usmap: skip package version: %w", err)
	}
	count, err := r.int32()
	if err != nil {
		return fmt.Errorf("usmap: read custom version count: %w", err)
	}
	if count < 0 || int(count) > usmapMaxEnums {
		return fmt.Errorf("usmap: custom version count %d out of range", count)
	}
	// Each FCustomVersion = FGuid(16 bytes) + int32 version = 20 bytes.
	if err := r.skip(int(count) * 20); err != nil {
		return fmt.Errorf("usmap: skip custom versions: %w", err)
	}
	if err := r.skip(4); err != nil { // NetCL uint32
		return fmt.Errorf("usmap: skip NetCL: %w", err)
	}
	return nil
}

// decompressUsmap turns the compressed body into the raw mappings bytes,
// honoring the declared method. The result is verified to match decompSize.
func decompressUsmap(method int, comp []byte, decompSize int) ([]byte, error) {
	switch method {
	case usmapNone:
		if len(comp) != decompSize {
			return nil, fmt.Errorf("usmap: None compression requires comp==decomp (%d != %d)", len(comp), decompSize)
		}
		// Copy so the body owns its bytes independent of the input buffer.
		out := make([]byte, decompSize)
		copy(out, comp)
		return out, nil

	case usmapZStandard:
		dec, err := zstd.NewReader(nil,
			zstd.WithDecoderMaxMemory(zstdMaxDecoderMem),
			zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("usmap: zstd init: %w", err)
		}
		defer dec.Close()
		// DecodeAll with a pre-sized (and clamped) destination caps the output:
		// decompSize was already validated against usmapMaxDecompSize.
		dst := make([]byte, 0, decompSize)
		out, err := dec.DecodeAll(comp, dst)
		if err != nil {
			return nil, fmt.Errorf("usmap: zstd decode: %w", err)
		}
		if len(out) != decompSize {
			return nil, fmt.Errorf("usmap: zstd decoded %d bytes, header declared %d", len(out), decompSize)
		}
		return out, nil

	case usmapOodle:
		return nil, ErrUsmapOodle

	case usmapBrotli:
		return nil, fmt.Errorf("usmap: Brotli-compressed mappings not yet supported offline")

	default:
		return nil, fmt.Errorf("usmap: unknown compression method %d", method)
	}
}

// parseUsmapBody parses the decompressed name/enum/struct tables into out.
func parseUsmapBody(out *UsmapData, body []byte) error {
	r := &byteReader{data: body}

	// --- Name table ---
	nameCount, err := r.uint32()
	if err != nil {
		return fmt.Errorf("usmap: read name count: %w", err)
	}
	if nameCount > usmapMaxNames {
		return fmt.Errorf("usmap: name count %d exceeds cap %d", nameCount, usmapMaxNames)
	}
	names := make([]string, 0, nameCount)
	for i := uint32(0); i < nameCount; i++ {
		s, err := r.usmapName(out.Version)
		if err != nil {
			return fmt.Errorf("usmap: name[%d]: %w", i, err)
		}
		names = append(names, s)
	}
	out.Names = names

	resolve := func(idx int32) (string, error) {
		if idx == -1 {
			return "", nil
		}
		if idx < 0 || int(idx) >= len(names) {
			return "", fmt.Errorf("usmap: name index %d out of range (have %d)", idx, len(names))
		}
		return names[idx], nil
	}

	// --- Enum table ---
	enumCount, err := r.uint32()
	if err != nil {
		return fmt.Errorf("usmap: read enum count: %w", err)
	}
	if enumCount > usmapMaxEnums {
		return fmt.Errorf("usmap: enum count %d exceeds cap %d", enumCount, usmapMaxEnums)
	}
	out.Enums = make([]UsmapEnum, 0, enumCount)
	for i := uint32(0); i < enumCount; i++ {
		nameIdx, err := r.int32()
		if err != nil {
			return fmt.Errorf("usmap: enum[%d] name: %w", i, err)
		}
		enumName, err := resolve(nameIdx)
		if err != nil {
			return fmt.Errorf("usmap: enum[%d] name: %w", i, err)
		}

		var entryCount uint32
		if out.Version >= usmapVerLargeEnums {
			v, err := r.uint16()
			if err != nil {
				return fmt.Errorf("usmap: enum[%d] entry count: %w", i, err)
			}
			entryCount = uint32(v)
		} else {
			v, err := r.uint8()
			if err != nil {
				return fmt.Errorf("usmap: enum[%d] entry count: %w", i, err)
			}
			entryCount = uint32(v)
		}
		if entryCount > usmapMaxEnumVals {
			return fmt.Errorf("usmap: enum[%d] has %d entries, exceeds cap %d", i, entryCount, usmapMaxEnumVals)
		}

		e := UsmapEnum{Name: enumName, Names: make(map[uint64]string, entryCount)}
		for j := uint32(0); j < entryCount; j++ {
			var value uint64
			if out.Version >= usmapVerExplicitEnums {
				value, err = r.uint64()
				if err != nil {
					return fmt.Errorf("usmap: enum[%d] value[%d]: %w", i, j, err)
				}
			} else {
				value = uint64(j)
			}
			vIdx, err := r.int32()
			if err != nil {
				return fmt.Errorf("usmap: enum[%d] value name[%d]: %w", i, j, err)
			}
			vName, err := resolve(vIdx)
			if err != nil {
				return fmt.Errorf("usmap: enum[%d] value name[%d]: %w", i, j, err)
			}
			e.Names[value] = vName
		}
		out.Enums = append(out.Enums, e)
	}

	// --- Struct/schema table ---
	structCount, err := r.uint32()
	if err != nil {
		return fmt.Errorf("usmap: read struct count: %w", err)
	}
	if structCount > usmapMaxStructs {
		return fmt.Errorf("usmap: struct count %d exceeds cap %d", structCount, usmapMaxStructs)
	}
	out.Structs = make([]UsmapStruct, 0, structCount)
	for i := uint32(0); i < structCount; i++ {
		s, err := parseUsmapStruct(r, out.Version, resolve)
		if err != nil {
			return fmt.Errorf("usmap: struct[%d]: %w", i, err)
		}
		out.Structs = append(out.Structs, s)
	}
	return nil
}

// parseUsmapStruct parses one UStruct schema.
func parseUsmapStruct(r *byteReader, version int, resolve func(int32) (string, error)) (UsmapStruct, error) {
	var s UsmapStruct

	nameIdx, err := r.int32()
	if err != nil {
		return s, fmt.Errorf("name: %w", err)
	}
	if s.Name, err = resolve(nameIdx); err != nil {
		return s, fmt.Errorf("name: %w", err)
	}

	superIdx, err := r.int32()
	if err != nil {
		return s, fmt.Errorf("super: %w", err)
	}
	if s.SuperType, err = resolve(superIdx); err != nil {
		return s, fmt.Errorf("super: %w", err)
	}

	// propertyCount is the flattened slot total (incl. fixed-array expansion);
	// serializablePropertyCount is how many property records actually follow.
	if _, err := r.uint16(); err != nil { // propertyCount (unused: we count records)
		return s, fmt.Errorf("property count: %w", err)
	}
	serCount, err := r.uint16()
	if err != nil {
		return s, fmt.Errorf("serializable count: %w", err)
	}
	if uint32(serCount) > usmapMaxProps {
		return s, fmt.Errorf("serializable count %d exceeds cap %d", serCount, usmapMaxProps)
	}

	s.Properties = make([]UsmapProperty, 0, serCount)
	for i := 0; i < int(serCount); i++ {
		p, err := parseUsmapProperty(r, version, resolve)
		if err != nil {
			return s, fmt.Errorf("property[%d]: %w", i, err)
		}
		s.Properties = append(s.Properties, p)
	}
	return s, nil
}

// parseUsmapProperty parses one property record.
func parseUsmapProperty(r *byteReader, version int, resolve func(int32) (string, error)) (UsmapProperty, error) {
	var p UsmapProperty

	idx, err := r.uint16()
	if err != nil {
		return p, fmt.Errorf("index: %w", err)
	}
	p.Index = int(idx)

	arrayDim, err := r.uint8()
	if err != nil {
		return p, fmt.Errorf("array dim: %w", err)
	}
	p.ArrayDim = int(arrayDim)

	nameIdx, err := r.int32()
	if err != nil {
		return p, fmt.Errorf("name: %w", err)
	}
	if p.Name, err = resolve(nameIdx); err != nil {
		return p, fmt.Errorf("name: %w", err)
	}

	t, err := parseUsmapPropertyType(r, version, resolve, 0)
	if err != nil {
		return p, fmt.Errorf("type: %w", err)
	}
	p.Type = t
	return p, nil
}

// parseUsmapPropertyType parses a (recursive) property type. depth guards
// against a maliciously deep nest driving stack exhaustion.
func parseUsmapPropertyType(r *byteReader, version int, resolve func(int32) (string, error), depth int) (UsmapPropertyType, error) {
	var t UsmapPropertyType
	if depth > usmapMaxNestDepth {
		return t, fmt.Errorf("property type nested deeper than %d", usmapMaxNestDepth)
	}

	typeByte, err := r.uint8()
	if err != nil {
		return t, fmt.Errorf("type tag: %w", err)
	}
	if name, ok := propertyTypeNames[int(typeByte)]; ok {
		t.Type = name
	} else {
		t.Type = fmt.Sprintf("Unknown(%d)", typeByte)
	}

	switch int(typeByte) {
	case ptEnum:
		inner, err := parseUsmapPropertyType(r, version, resolve, depth+1)
		if err != nil {
			return t, err
		}
		t.Inner = &inner
		nameIdx, err := r.int32()
		if err != nil {
			return t, fmt.Errorf("enum name: %w", err)
		}
		if t.EnumName, err = resolve(nameIdx); err != nil {
			return t, fmt.Errorf("enum name: %w", err)
		}
	case ptStruct:
		nameIdx, err := r.int32()
		if err != nil {
			return t, fmt.Errorf("struct name: %w", err)
		}
		if t.StructType, err = resolve(nameIdx); err != nil {
			return t, fmt.Errorf("struct name: %w", err)
		}
	case ptArray, ptSet, ptOptional:
		inner, err := parseUsmapPropertyType(r, version, resolve, depth+1)
		if err != nil {
			return t, err
		}
		t.Inner = &inner
	case ptMap:
		inner, err := parseUsmapPropertyType(r, version, resolve, depth+1)
		if err != nil {
			return t, err
		}
		t.Inner = &inner
		value, err := parseUsmapPropertyType(r, version, resolve, depth+1)
		if err != nil {
			return t, err
		}
		t.Value = &value
	}
	return t, nil
}

// usmapName reads one entry of the name table: a length (uint8 pre-LongFName,
// uint16 from LongFName onward) followed by that many ASCII bytes.
func (r *byteReader) usmapName(version int) (string, error) {
	var n int
	if version >= usmapVerLongFName {
		v, err := r.uint16()
		if err != nil {
			return "", err
		}
		n = int(v)
	} else {
		v, err := r.uint8()
		if err != nil {
			return "", err
		}
		n = int(v)
	}
	if n > usmapMaxStringLen {
		return "", fmt.Errorf("name length %d exceeds cap %d", n, usmapMaxStringLen)
	}
	if err := r.need(n); err != nil {
		return "", err
	}
	s := string(r.data[r.pos : r.pos+n])
	r.pos += n
	return s, nil
}

// Fixed-width little-endian readers used by the usmap parser. They mirror the
// bounds-checked style of int32/uint32 in uasset.go (same byteReader type).

func (r *byteReader) uint8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	v := r.data[r.pos]
	r.pos++
	return v, nil
}

func (r *byteReader) uint16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *byteReader) uint64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}
