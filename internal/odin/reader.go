package odin

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

// reader is a little-endian cursor over an Odin BinaryDataReader blob.
// Mirrors the C# odindec Reader class.
type reader struct {
	b []byte
	i int
}

func newReader(data []byte) *reader { return &reader{b: data, i: 0} }

func (r *reader) end() bool      { return r.i >= len(r.b) }
func (r *reader) pos() int       { return r.i }
func (r *reader) peekType() byte { return r.b[r.i] }

func (r *reader) readByteRaw() byte { v := r.b[r.i]; r.i++; return v }
func (r *reader) readSByte() int8   { return int8(r.readByteRaw()) }

func (r *reader) readInt16() int16    { v := int16(binary.LittleEndian.Uint16(r.b[r.i:])); r.i += 2; return v }
func (r *reader) readUInt16() uint16  { v := binary.LittleEndian.Uint16(r.b[r.i:]); r.i += 2; return v }
func (r *reader) readInt32() int32    { v := int32(binary.LittleEndian.Uint32(r.b[r.i:])); r.i += 4; return v }
func (r *reader) readUInt32() uint32  { v := binary.LittleEndian.Uint32(r.b[r.i:]); r.i += 4; return v }
func (r *reader) readInt64() int64    { v := int64(binary.LittleEndian.Uint64(r.b[r.i:])); r.i += 8; return v }
func (r *reader) readUInt64() uint64  { v := binary.LittleEndian.Uint64(r.b[r.i:]); r.i += 8; return v }
func (r *reader) readSingle() float32 { v := math.Float32frombits(binary.LittleEndian.Uint32(r.b[r.i:])); r.i += 4; return v }
func (r *reader) readDouble() float64 { v := math.Float64frombits(binary.LittleEndian.Uint64(r.b[r.i:])); r.i += 8; return v }
func (r *reader) readChar() string    { v := binary.LittleEndian.Uint16(r.b[r.i:]); r.i += 2; return string(utf16.Decode([]uint16{v})) }

// readString decodes Odin WriteStringFast: [flag:1][int32 count][data].
// flag 1 = UTF16LE (count*2 bytes); flag 0 = 8-bit one byte per char.
func (r *reader) readString() string {
	flag := r.readByteRaw()
	count := int(r.readInt32())
	if count < 0 || count > 5_000_000 {
		panic(fmt.Sprintf("bad string len %d at %d", count, r.i))
	}
	if flag == 1 {
		u := make([]uint16, count)
		for k := 0; k < count; k++ {
			u[k] = binary.LittleEndian.Uint16(r.b[r.i+k*2:])
		}
		r.i += count * 2
		return string(utf16.Decode(u))
	}
	var sb strings.Builder
	sb.Grow(count)
	for k := 0; k < count; k++ {
		sb.WriteRune(rune(r.b[r.i+k]))
	}
	r.i += count
	return sb.String()
}
