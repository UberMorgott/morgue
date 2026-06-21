package odin

import "strconv"

// tok is one decoded token. Mirrors the C# Tok class.
type tok struct {
	kind string // node-start, node-end, array-start, array-end, primarray,
	// int, uint, long, ulong, float, double, string, bool,
	// sbyte, byte, short, ushort, char, null, internalref,
	// extref, eos, raw
	name    string // field name ("" for unnamed)
	value   any    // numeric/string value
	typeStr string // node-start: type name; primarray: bytesPer as string
	rawArr  []byte // primarray raw bytes
	depth   int
}

// decoder walks an Odin blob into a flat token list. Mirrors C# Decoder.
type decoder struct {
	r         *reader
	toks      []tok
	depth     int
	typeCache map[int32]string
}

func newDecoder(data []byte) *decoder {
	return &decoder{r: newReader(data), typeCache: map[int32]string{}}
}

// readType handles WriteType markers: 0x2F TypeName, 0x30 TypeID, 0x2E UnnamedNull.
func (d *decoder) readType() string {
	if d.r.end() {
		return ""
	}
	t := d.r.peekType()
	switch t {
	case 0x2F:
		d.r.readByteRaw()
		id := d.r.readInt32()
		s := d.r.readString()
		d.typeCache[id] = s
		return s
	case 0x30:
		d.r.readByteRaw()
		id := d.r.readInt32()
		if v, ok := d.typeCache[id]; ok {
			return v
		}
		return "#id" + itoa(int(id))
	case 0x2E:
		d.r.readByteRaw()
		return ""
	}
	return ""
}

func (d *decoder) add(t tok) { d.toks = append(d.toks, t) }
func (d *decoder) addVal(kind, name string, v any) {
	d.add(tok{kind: kind, name: name, value: v, depth: d.depth})
}

func (d *decoder) skipN(n int) {
	for k := 0; k < n && !d.r.end(); k++ {
		d.r.readByteRaw()
	}
}

func (d *decoder) run() {
	for !d.r.end() {
		t := d.r.readByteRaw()
		switch t {
		case 0x01: // NamedStartOfReferenceNode
			name := d.r.readString()
			ty := d.readType()
			d.r.readInt32() // id
			d.add(tok{kind: "node-start", name: name, typeStr: ty, depth: d.depth})
			d.depth++
		case 0x02: // UnnamedStartOfReferenceNode
			ty := d.readType()
			d.r.readInt32()
			d.add(tok{kind: "node-start", typeStr: ty, depth: d.depth})
			d.depth++
		case 0x03: // NamedStartOfStructNode
			name := d.r.readString()
			ty := d.readType()
			d.add(tok{kind: "node-start", name: name, typeStr: ty, depth: d.depth})
			d.depth++
		case 0x04: // UnnamedStartOfStructNode
			ty := d.readType()
			d.add(tok{kind: "node-start", typeStr: ty, depth: d.depth})
			d.depth++
		case 0x05: // EndOfNode
			d.depth--
			d.add(tok{kind: "node-end", depth: d.depth})
		case 0x06: // StartOfArray (int64 length)
			length := d.r.readInt64()
			d.add(tok{kind: "array-start", value: length, depth: d.depth})
			d.depth++
		case 0x07: // EndOfArray
			d.depth--
			d.add(tok{kind: "array-end", depth: d.depth})
		case 0x08: // PrimitiveArray: int32 count, int32 bytesPer, raw bytes
			count := int(d.r.readInt32())
			bytesPer := int(d.r.readInt32())
			raw := make([]byte, count*bytesPer)
			for k := 0; k < len(raw) && !d.r.end(); k++ {
				raw[k] = d.r.readByteRaw()
			}
			d.add(tok{kind: "primarray", value: int64(count), typeStr: itoa(bytesPer), rawArr: raw, depth: d.depth})
		case 0x09: // NamedInternalReference
			nm := d.r.readString()
			id := d.r.readInt32()
			d.add(tok{kind: "internalref", name: nm, value: id, depth: d.depth})
		case 0x0A:
			d.r.readInt32()
			d.add(tok{kind: "internalref", depth: d.depth})
		case 0x0B:
			nm := d.r.readString()
			id := d.r.readInt32()
			d.add(tok{kind: "extref", name: nm, value: id, depth: d.depth})
		case 0x0C:
			id := d.r.readInt32()
			d.add(tok{kind: "extref", value: id, depth: d.depth})
		case 0x0D:
			nm := d.r.readString()
			d.skipN(16) // guid
			d.add(tok{kind: "extref", name: nm, depth: d.depth})
		case 0x0E:
			d.skipN(16)
			d.add(tok{kind: "extref", depth: d.depth})
		case 0x0F:
			nm := d.r.readString()
			d.addVal("sbyte", nm, d.r.readSByte())
		case 0x10:
			d.addVal("sbyte", "", d.r.readSByte())
		case 0x11:
			nm := d.r.readString()
			d.addVal("byte", nm, d.r.readByteRaw())
		case 0x12:
			d.addVal("byte", "", d.r.readByteRaw())
		case 0x13:
			nm := d.r.readString()
			d.addVal("short", nm, d.r.readInt16())
		case 0x14:
			d.addVal("short", "", d.r.readInt16())
		case 0x15:
			nm := d.r.readString()
			d.addVal("ushort", nm, d.r.readUInt16())
		case 0x16:
			d.addVal("ushort", "", d.r.readUInt16())
		case 0x17: // NamedInt
			nm := d.r.readString()
			d.addVal("int", nm, d.r.readInt32())
		case 0x18:
			d.addVal("int", "", d.r.readInt32())
		case 0x19:
			nm := d.r.readString()
			d.addVal("uint", nm, d.r.readUInt32())
		case 0x1A:
			d.addVal("uint", "", d.r.readUInt32())
		case 0x1B:
			nm := d.r.readString()
			d.addVal("long", nm, d.r.readInt64())
		case 0x1C:
			d.addVal("long", "", d.r.readInt64())
		case 0x1D: // NamedULong
			nm := d.r.readString()
			d.addVal("ulong", nm, d.r.readUInt64())
		case 0x1E:
			d.addVal("ulong", "", d.r.readUInt64())
		case 0x1F: // NamedFloat
			nm := d.r.readString()
			d.addVal("float", nm, d.r.readSingle())
		case 0x20:
			d.addVal("float", "", d.r.readSingle())
		case 0x21:
			nm := d.r.readString()
			d.addVal("double", nm, d.r.readDouble())
		case 0x22:
			d.addVal("double", "", d.r.readDouble())
		case 0x23:
			nm := d.r.readString()
			d.skipN(16) // decimal
			d.addVal("decimal", nm, "?")
		case 0x24:
			d.skipN(16)
			d.addVal("decimal", "", "?")
		case 0x25:
			nm := d.r.readString()
			d.addVal("char", nm, d.r.readChar())
		case 0x26:
			d.addVal("char", "", d.r.readChar())
		case 0x27: // NamedString
			nm := d.r.readString()
			d.addVal("string", nm, d.r.readString())
		case 0x28:
			d.addVal("string", "", d.r.readString())
		case 0x29:
			nm := d.r.readString()
			d.skipN(16)
			d.addVal("guid", nm, "?")
		case 0x2A:
			d.skipN(16)
			d.addVal("guid", "", "?")
		case 0x2B:
			nm := d.r.readString()
			d.addVal("bool", nm, d.r.readByteRaw() != 0)
		case 0x2C:
			d.addVal("bool", "", d.r.readByteRaw() != 0)
		case 0x2D:
			nm := d.r.readString()
			d.addVal("null", nm, nil)
		case 0x2E:
			d.add(tok{kind: "null", depth: d.depth})
		case 0x2F: // stray TypeName
			d.r.readInt32()
			d.r.readString()
		case 0x30: // stray TypeID
			d.r.readInt32()
		case 0x31: // EndOfStream
			d.add(tok{kind: "eos", depth: d.depth})
		case 0x32:
			nm := d.r.readString()
			d.r.readString()
			d.add(tok{kind: "extref", name: nm, depth: d.depth})
		case 0x33:
			d.r.readString()
			d.add(tok{kind: "extref", depth: d.depth})
		default:
			d.add(tok{kind: "raw", value: t, depth: d.depth})
		}
	}
}

func itoa(v int) string { return strconv.Itoa(v) }
