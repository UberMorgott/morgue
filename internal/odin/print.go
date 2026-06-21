package odin

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// printTree renders the token list to the exact text layout produced by the
// C# odindec PrintTree (newline-terminated; trailing newline included).
func printTree(toks []tok) string {
	var sb strings.Builder
	var typeStack []string
	for _, t := range toks {
		ind := strings.Repeat(" ", max0(t.depth)*2)
		switch t.kind {
		case "node-start":
			typeStack = append(typeStack, t.typeStr)
			sb.WriteString(fmt.Sprintf("%s%s { %s\n", ind, nameOrDot(t.name), shortType(t.typeStr)))
		case "primarray":
			ptype := ""
			if len(typeStack) > 0 {
				ptype = typeStack[len(typeStack)-1]
			}
			bytesPer, _ := strconv.Atoi(t.typeStr)
			sb.WriteString(fmt.Sprintf("%s%s\n", ind, decodePrimArray(t.rawArr, bytesPer, ptype)))
		case "node-end":
			if len(typeStack) > 0 {
				typeStack = typeStack[:len(typeStack)-1]
			}
			sb.WriteString(fmt.Sprintf("%s}\n", ind))
		case "array-start":
			sb.WriteString(fmt.Sprintf("%s[array len=%v]\n", ind, t.value))
		case "array-end":
			sb.WriteString(fmt.Sprintf("%s[/array]\n", ind))
		case "int", "uint", "long", "ulong", "float", "double", "bool", "string",
			"byte", "sbyte", "short", "ushort", "char":
			sb.WriteString(fmt.Sprintf("%s%s = %s  (%s)\n", ind, nameOrDot(t.name), fmtVal(t), t.kind))
		case "null":
			if t.name != "" {
				sb.WriteString(fmt.Sprintf("%s%s = null\n", ind, t.name))
			}
		case "eos":
			// no output
		default:
			// raw/internalref/extref: no output (matches C# default)
		}
	}
	return sb.String()
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func nameOrDot(name string) string {
	if name == "" {
		return "·" // middle dot, matches C# "·"
	}
	return name
}

// fmtVal formats a scalar token's value with invariant-culture rules.
func fmtVal(t tok) string {
	if t.value == nil {
		return "null"
	}
	switch t.kind {
	case "float":
		return trimFloat(float64(t.value.(float32)))
	case "double":
		return trimFloat(t.value.(float64))
	case "bool":
		if t.value.(bool) {
			return "True"
		}
		return "False"
	case "string":
		return t.value.(string)
	case "char":
		return t.value.(string)
	}
	return fmt.Sprintf("%v", t.value)
}

// trimFloat reproduces C# ToString("0.######", InvariantCulture):
// up to 6 fractional digits, trailing zeros removed, no trailing dot.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// shortType collapses a node's type name for readability (C# ShortType).
func shortType(ty string) string {
	if ty == "" {
		return ""
	}
	if strings.Contains(ty, "Dictionary`2") {
		return "<Dict>"
	}
	if strings.Contains(ty, "List`1") {
		return "<List>"
	}
	s := ty
	if comma := strings.Index(ty, ", "); comma > 0 {
		s = ty[:comma]
	}
	if dot := strings.LastIndex(s, "."); dot > 0 && dot < len(s)-1 {
		s = s[dot+1:]
	}
	return "<" + s + ">"
}

// decodePrimArray formats a primitive array (C# DecodePrimArray).
func decodePrimArray(raw []byte, bytesPer int, ptype string) string {
	if bytesPer < 1 {
		bytesPer = 1
	}
	count := len(raw) / bytesPer
	isFloat := strings.Contains(ptype, "Single")
	isDouble := strings.Contains(ptype, "Double")
	isInt := strings.Contains(ptype, "Int32")
	isLong := strings.Contains(ptype, "Int64")
	vals := make([]string, 0, count)
	for k := 0; k < count; k++ {
		off := k * bytesPer
		switch {
		case isFloat && bytesPer == 4:
			vals = append(vals, trimFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[off:])))))
		case isDouble && bytesPer == 8:
			vals = append(vals, trimFloat(math.Float64frombits(binary.LittleEndian.Uint64(raw[off:]))))
		case isLong && bytesPer == 8:
			vals = append(vals, strconv.FormatInt(int64(binary.LittleEndian.Uint64(raw[off:])), 10))
		case bytesPer == 4:
			vals = append(vals, strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(raw[off:]))), 10))
		case bytesPer == 8:
			vals = append(vals, strconv.FormatInt(int64(binary.LittleEndian.Uint64(raw[off:])), 10))
		case bytesPer == 2:
			vals = append(vals, strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(raw[off:]))), 10))
		default:
			vals = append(vals, strconv.FormatInt(int64(raw[off]), 10))
		}
	}
	kind := "b" + strconv.Itoa(bytesPer) + "[]"
	switch {
	case isFloat:
		kind = "float[]"
	case isDouble:
		kind = "double[]"
	case isLong:
		kind = "long[]"
	case isInt:
		kind = "int[]"
	}
	return fmt.Sprintf("= [%s]  (%s n=%d)", strings.Join(vals, ", "), kind, count)
}
