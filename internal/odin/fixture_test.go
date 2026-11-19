package odin

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// enc hand-encodes Odin BinaryDataWriter tokens for synthetic test fixtures.
type enc struct{ b []byte }

func (e *enc) op(ops ...byte) *enc { e.b = append(e.b, ops...); return e }
func (e *enc) i32(v int32) *enc {
	e.b = binary.LittleEndian.AppendUint32(e.b, uint32(v)) //nolint:gosec // raw bits of a signed test value
	return e
}
func (e *enc) i64(v int64) *enc {
	e.b = binary.LittleEndian.AppendUint64(e.b, uint64(v)) //nolint:gosec // raw bits of a signed test value
	return e
}
func (e *enc) u16(v uint16) *enc  { e.b = binary.LittleEndian.AppendUint16(e.b, v); return e }
func (e *enc) u32(v uint32) *enc  { e.b = binary.LittleEndian.AppendUint32(e.b, v); return e }
func (e *enc) u64(v uint64) *enc  { e.b = binary.LittleEndian.AppendUint64(e.b, v); return e }
func (e *enc) f32(v float32) *enc { return e.u32(math.Float32bits(v)) }
func (e *enc) f64(v float64) *enc { return e.u64(math.Float64bits(v)) }

// str writes a UTF-16 string (flag 1); str8 an 8-bit one (flag 0). ASCII only.
func (e *enc) str(s string) *enc {
	e.op(0x01).i32(int32(len(s))) //nolint:gosec // short test literal
	for _, c := range s {
		e.u16(uint16(c)) //nolint:gosec // ASCII test literal
	}
	return e
}
func (e *enc) str8(s string) *enc { e.op(0x00).i32(int32(len(s))); return e.op([]byte(s)...) } //nolint:gosec // short test literal

// typeName writes a 0x2F TypeName entry (caches id), typeID a 0x30 cache lookup.
func (e *enc) typeName(id int32, name string) *enc { return e.op(0x2F).i32(id).str(name) }
func (e *enc) typeID(id int32) *enc                { return e.op(0x30).i32(id) }

// primArray writes a 0x08 PrimitiveArray: count, bytesPer, raw payload.
func (e *enc) primArray(bytesPer int32, raw []byte) *enc {
	return e.op(0x08).i32(int32(len(raw)) / bytesPer).i32(bytesPer).op(raw...) //nolint:gosec // short test payload
}

func le32s(vs ...uint32) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}

func le64s(vs ...uint64) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint64(b, v)
	}
	return b
}

// sampleBlob is a synthetic Odin blob shaped like a SerializedScriptableObject:
// a typed _data reference node holding a Dictionary, a List of typed refs (type
// cache hit via TypeID), typed primitive arrays, every named scalar, an unnamed
// struct of every unnamed scalar/ref, stray type markers, a raw byte and EOS.
func sampleBlob() []byte {
	guid := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	e := &enc{}
	e.op(0x01).str("_data").typeName(0, "Sample.Asset+Data, Assembly-CSharp").i32(0)
	e.op(0x17).str("mCost").i32(1)

	e.op(0x01).str("mScoreDic").typeName(1, "System.Collections.Generic.Dictionary`2[[System.Int32],[System.Int32]], mscorlib").i32(1)
	e.op(0x06).i64(2)
	e.op(0x04, 0x2E).op(0x18).i32(1).op(0x18).i32(10).op(0x05)
	e.op(0x04, 0x2E).op(0x18).i32(2).op(0x18).i32(20).op(0x05)
	e.op(0x07, 0x05)

	e.op(0x01).str("mRateList").typeName(2, "System.Collections.Generic.List`1[[Sample.Rate, Assembly-CSharp]], mscorlib").i32(2)
	e.op(0x06).i64(2)
	e.op(0x02).typeName(3, "Sample.Rate, Assembly-CSharp").i32(3)
	e.op(0x17).str("mScore").i32(10).op(0x1F).str("mRate").f32(0.35).op(0x05)
	e.op(0x02).typeID(3).i32(4)
	e.op(0x17).str("mScore").i32(20).op(0x1F).str("mRate").f32(151.13637).op(0x05)
	e.op(0x07, 0x05)

	e.op(0x03).str("floats").typeName(4, "System.Single[], mscorlib").primArray(4, le32s(math.Float32bits(1.5), math.Float32bits(-0.25))).op(0x05)
	e.op(0x03).str("doubles").typeName(5, "System.Double[], mscorlib").primArray(8, le64s(math.Float64bits(2.125))).op(0x05)
	e.op(0x03).str("ints").typeName(6, "System.Int32[], mscorlib").primArray(4, le32s(7, 0xFFFFFFFF)).op(0x05)
	e.op(0x03).str("longs").typeName(7, "System.Int64[], mscorlib").primArray(8, le64s(1<<40)).op(0x05)
	e.op(0x03).str("bytes").typeName(8, "System.Byte[], mscorlib").primArray(1, []byte{0xDE, 0xAD}).op(0x05)
	e.op(0x03).str("shorts").typeID(99).primArray(2, []byte{0xFE, 0xFF, 0x05, 0x00}).op(0x05)
	e.op(0x03).str("raw8").op(0x2E).primArray(8, le64s(3)).op(0x05)
	e.op(0x03).str("raw4").op(0x2E).primArray(4, le32s(9)).op(0x05)
	e.op(0x03).str("raw3").op(0x2E).primArray(3, []byte{1, 2, 3}).op(0x05)

	e.op(0x0F).str("sb").op(0xFF)
	e.op(0x11).str("by").op(0xC8)
	e.op(0x13).str("sh").u16(0xFFFD)
	e.op(0x15).str("us").u16(65535)
	e.op(0x19).str("ui").u32(4000000000)
	e.op(0x1B).str("lo").i64(-5)
	e.op(0x1D).str("ul").u64(18446744073709551615)
	e.op(0x21).str("do").f64(0.1234567891)
	e.op(0x23).str("de").op(make([]byte, 16)...)
	e.op(0x25).str("ch").u16('Z')
	e.op(0x27).str("st").str8("hello")
	e.op(0x29).str("gu").op(guid...)
	e.op(0x2B).str("bo").op(0x01)
	e.op(0x2D).str("nu")
	e.op(0x09).str("iref").i32(5)
	e.op(0x0B).str("xref").i32(6)
	e.op(0x0D).str("gref").op(guid...)
	e.op(0x32).str("sref").str("ref/path")

	e.op(0x04, 0x2E)
	e.op(0x10, 0x80).op(0x12, 0x07).op(0x14).u16(3).op(0x16).u16(4).op(0x1A).u32(5)
	e.op(0x1C).i64(6).op(0x1E).u64(7).op(0x20).f32(8.5).op(0x22).f64(9.5)
	e.op(0x24).op(make([]byte, 16)...).op(0x26).u16('q').op(0x28).str("u")
	e.op(0x2A).op(guid...).op(0x2C, 0x00).op(0x2E)
	e.op(0x0A).i32(1).op(0x0C).i32(2).op(0x0E).op(guid...).op(0x33).str("p")
	e.op(0x05)

	e.typeName(50, "Stray").typeID(50) // stray type markers at top level
	e.op(0xFF)                         // unknown opcode -> raw token
	e.op(0x05, 0x31)                   // close _data, EndOfStream
	return e.b
}

// writeOdinAsset writes a Unity SerializedScriptableObject .asset envelope whose
// serializationData.SerializedBytes carries blob.
func writeOdinAsset(t *testing.T, dir, name, guid string, blob []byte) string {
	t.Helper()
	body := "%YAML 1.1\n" +
		"%TAG !u! tag:unity3d.com,2011:\n" +
		"--- !u!114 &11400000\n" +
		"MonoBehaviour:\n" +
		"  m_ObjectHideFlags: 0\n" +
		"  m_Enabled: 1\n" +
		"  m_Script: {fileID: 1234567, guid: " + guid + ", type: 3}\n" +
		"  m_Name: " + name + "\n" +
		"  serializationData:\n" +
		"    SerializedFormat: 0\n" +
		"    SerializedBytes: " + hex.EncodeToString(blob) + "\n" +
		"    SerializedBytesString:\n"
	p := filepath.Join(dir, name+".asset")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}
