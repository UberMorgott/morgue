package quality

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestClassInventoryCodeVsAbstract(t *testing.T) {
	var b bytes.Buffer
	put := func(v any) {
		if err := binary.Write(&b, binary.BigEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	put(uint32(0xcafebabe))
	put(uint16(0))
	put(uint16(61))
	put(uint16(5))
	for _, s := range []string{"Demo", "run", "()V", "Code"} {
		put(uint8(1))
		put(uint16(len(s)))
		b.WriteString(s)
	}
	for _, n := range []uint16{1, 1, 0, 0, 0, 2} {
		put(n)
	} // access,this,super,interfaces,fields,methods
	for _, flags := range []uint16{1, 0x401} {
		put(flags)
		put(uint16(2))
		put(uint16(3))
		if flags == 1 {
			put(uint16(1))
			put(uint16(4))
			put(uint32(0))
		} else {
			put(uint16(0))
		}
	}
	put(uint16(0))
	inv, err := readClass(bytes.NewReader(b.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Types != 1 || inv.Methods != 1 || inv.ExcludedMethods != 1 {
		t.Fatalf("%+v", inv)
	}
	if _, err := readClass(bytes.NewReader(b.Bytes()[:20]), nil); err == nil {
		t.Fatal("truncation accepted")
	}
}
