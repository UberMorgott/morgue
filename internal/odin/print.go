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
		return fmtFloatCap6(float64(t.value.(float32)), 32)
	case "double":
		return fmtFloatCap6(t.value.(float64), 64)
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

// fmtFloatCap6 reproduces .NET Core ToString("0.######", InvariantCulture).
//
// .NET formats the value to its display precision FIRST, then the "0.######"
// custom format caps the result to at most 6 fractional digits:
//   - float (bitSize 32): display precision is 7 significant digits (G7), so a
//     float32 whose shortest round-trip is 151.13637 prints as 151.1364. Naively
//     widening float32->float64 and printing the shortest double (151.13637) or
//     6 fixed decimals (151.136368) is WRONG — verified against .NET net8.0.
//   - double (bitSize 64): full precision, then the 6-fractional-digit cap.
// After the precision step, trailing zeros and a trailing dot are removed.
func fmtFloatCap6(v float64, bitSize int) string {
	var s string
	if bitSize == 32 {
		s = strconv.FormatFloat(v, 'g', 7, 32) // 7 significant digits (.NET G7)
	} else {
		s = strconv.FormatFloat(v, 'g', -1, 64) // shortest round-trippable double
	}
	// Expand exponent form to plain decimal so fractional digits can be counted.
	if strings.ContainsAny(s, "eE") {
		s = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		if frac := len(s) - dot - 1; frac > 6 {
			rv, _ := strconv.ParseFloat(s, 64)
			s = strconv.FormatFloat(rv, 'f', 6, 64)
		}
	}
	if strings.ContainsRune(s, '.') {
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
			vals = append(vals, fmtFloatCap6(float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))), 32))
		case isDouble && bytesPer == 8:
			vals = append(vals, fmtFloatCap6(math.Float64frombits(binary.LittleEndian.Uint64(raw[off:])), 64))
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
