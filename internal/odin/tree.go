package odin

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// buildTree converts the flat token list produced by the decoder into a
// structured Go value suitable for JSON:
//
//   - reference/struct nodes  -> map[string]any (with "$type" when typed)
//   - arrays                  -> []any
//   - byte primitive arrays   -> hex string; other primarrays -> []any
//   - scalars                 -> int64/uint64/float64/string/bool
//   - external/internal refs  -> map[string]any{"$ref": "<id>"}
//
// It walks the SAME token stream as printTree; the text path is untouched.
func buildTree(toks []tok) map[string]any {
	b := &treeBuilder{toks: toks}
	root := map[string]any{}
	b.fillContainer(root)
	return root
}

type treeBuilder struct {
	toks      []tok
	i         int
	typeStack []string
}

// nextItem builds one logical value starting at the cursor. ok=false means the
// cursor sits on a container terminator (node-end/array-end) or end of stream.
func (b *treeBuilder) nextItem() (name string, val any, ok bool) {
	for b.i < len(b.toks) {
		t := b.toks[b.i]
		switch t.kind {
		case "eos", "raw":
			b.i++
			continue
		case "node-end", "array-end":
			return "", nil, false
		case "node-start":
			b.i++
			return t.name, b.buildNodeBody(t), true
		case "array-start":
			b.i++
			return t.name, b.buildArrayBody(), true
		case "primarray":
			b.i++
			return t.name, b.primArrayValue(t), true
		case "internalref":
			b.i++
			return t.name, refValue(t, true), true
		case "extref":
			b.i++
			return t.name, refValue(t, false), true
		case "null":
			b.i++
			return t.name, nil, true
		default: // scalar kinds
			b.i++
			return t.name, scalarValue(t), true
		}
	}
	return "", nil, false
}

// fillContainer drains items into a map until a terminator/EOF. Named items
// become keys; unnamed items (e.g. a Dict/List's payload array) collect under
// "$items".
func (b *treeBuilder) fillContainer(m map[string]any) {
	var unnamed []any
	for {
		name, val, ok := b.nextItem()
		if !ok {
			break
		}
		if name != "" {
			m[name] = val
		} else {
			unnamed = append(unnamed, val)
		}
	}
	switch len(unnamed) {
	case 0:
	case 1:
		m["$items"] = unnamed[0]
	default:
		m["$items"] = unnamed
	}
}

func (b *treeBuilder) buildNodeBody(start tok) any {
	m := map[string]any{}
	if start.typeStr != "" {
		m["$type"] = start.typeStr
	}
	b.typeStack = append(b.typeStack, start.typeStr)
	b.fillContainer(m)
	b.typeStack = b.typeStack[:len(b.typeStack)-1]
	if b.i < len(b.toks) && b.toks[b.i].kind == "node-end" {
		b.i++
	}
	return m
}

func (b *treeBuilder) buildArrayBody() any {
	arr := []any{}
	for {
		_, val, ok := b.nextItem()
		if !ok {
			break
		}
		arr = append(arr, val)
	}
	if b.i < len(b.toks) && b.toks[b.i].kind == "array-end" {
		b.i++
	}
	return arr
}

func (b *treeBuilder) primArrayValue(t tok) any {
	ptype := ""
	if len(b.typeStack) > 0 {
		ptype = b.typeStack[len(b.typeStack)-1]
	}
	bytesPer, _ := strconv.Atoi(t.typeStr)
	return primArrayToValue(t.rawArr, bytesPer, ptype)
}

// scalarValue normalises a scalar token to a JSON-friendly native value.
func scalarValue(t tok) any {
	// A value whose Go type disagrees with the token kind means the decoder read a
	// malformed blob; fall through and hand back the raw value untouched.
	switch t.kind {
	case "int":
		if v, ok := t.value.(int32); ok {
			return int64(v)
		}
	case "uint":
		if v, ok := t.value.(uint32); ok {
			return uint64(v)
		}
	case "long":
		if v, ok := t.value.(int64); ok {
			return v
		}
	case "ulong":
		if v, ok := t.value.(uint64); ok {
			return v
		}
	case "short":
		if v, ok := t.value.(int16); ok {
			return int64(v)
		}
	case "ushort":
		if v, ok := t.value.(uint16); ok {
			return int64(v)
		}
	case "sbyte":
		if v, ok := t.value.(int8); ok {
			return int64(v)
		}
	case "byte":
		if v, ok := t.value.(byte); ok {
			return int64(v)
		}
	case "float":
		if v, ok := t.value.(float32); ok {
			return clean32(v)
		}
	case "double":
		if v, ok := t.value.(float64); ok {
			return v
		}
	case "bool":
		if v, ok := t.value.(bool); ok {
			return v
		}
	}
	// string/char, decimal/guid placeholders ("?") and anything unexpected
	return t.value
}

// refValue renders a reference token as {"$ref": "<id>"}. Internal references
// (intra-blob) are prefixed with "#" so they don't collide with def ids.
func refValue(t tok, internal bool) any {
	id := ""
	switch v := t.value.(type) {
	case nil:
	case string:
		id = v
	case int32:
		id = strconv.FormatInt(int64(v), 10)
	case int64:
		id = strconv.FormatInt(v, 10)
	default:
		id = fmt.Sprintf("%v", v)
	}
	if internal {
		id = "#" + id
	}
	return map[string]any{"$ref": id}
}

// clean32 widens a float32 to float64 via its shortest decimal representation so
// JSON shows 0.35 rather than 0.3499999940395355.
func clean32(f float32) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}

// primArrayToValue decodes a primitive array. A 1-byte-per element array is a
// byte[] and is rendered as a hex string; wider arrays become []any of
// int64/float64 typed via the enclosing node type (same heuristic as the
// text printer's decodePrimArray).
func primArrayToValue(raw []byte, bytesPer int, ptype string) any {
	if bytesPer <= 1 {
		return hex.EncodeToString(raw)
	}
	count := len(raw) / bytesPer
	isFloat := strings.Contains(ptype, "Single")
	isDouble := strings.Contains(ptype, "Double")
	isLong := strings.Contains(ptype, "Int64")
	out := make([]any, 0, count)
	// The int casts below are deliberate two's-complement reinterpretations of
	// signed little-endian payloads, not value-range conversions.
	for k := range count {
		off := k * bytesPer
		switch {
		case isFloat && bytesPer == 4:
			out = append(out, clean32(math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))))
		case isDouble && bytesPer == 8:
			out = append(out, math.Float64frombits(binary.LittleEndian.Uint64(raw[off:])))
		case isLong && bytesPer == 8:
			out = append(out, int64(binary.LittleEndian.Uint64(raw[off:]))) //nolint:gosec // signed int64 payload read as its raw bits
		case bytesPer == 4:
			out = append(out, int64(int32(binary.LittleEndian.Uint32(raw[off:])))) //nolint:gosec // signed int32 payload read as its raw bits
		case bytesPer == 8:
			out = append(out, int64(binary.LittleEndian.Uint64(raw[off:]))) //nolint:gosec // signed int64 payload read as its raw bits
		case bytesPer == 2:
			out = append(out, int64(int16(binary.LittleEndian.Uint16(raw[off:])))) //nolint:gosec // signed int16 payload read as its raw bits
		default:
			out = append(out, int64(raw[off]))
		}
	}
	return out
}
