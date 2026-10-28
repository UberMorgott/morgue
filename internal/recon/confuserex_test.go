package recon

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	peparser "github.com/saferwall/pe"
)

func TestConfuserExDecrypterRequiresRelatedData(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      []byte
		initField uint32
		want      bool
	}{
		{"Cosmoteer-like dictionary getter with unrelated RVA", appendIL(nil, il{0x7e, 0x04000003}, il{0x7e, 0x04000003}, il{0x6f, 0x0a000001}), 2, false},
		{"byte-array getter with unrelated initializer", appendIL(nil, il{0x7e, 0x04000002}, il{0x7e, 0x04000003}, il{0x6f, 0x0a000001}), 4, false},
		{"decrypter reads initialized RVA byte array", appendIL(nil, il{0x7e, 0x04000002}, il{0x7e, 0x04000003}, il{0x6f, 0x0a000001}), 2, true},
		{"opcode-looking constants are not field loads", appendIL(nil, il{0x20, 0x6f7e7e00}), 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := decrypterFixture(tc.code, tc.initField)
			if got := detectConfuserExStringDecrypter(f); got != tc.want {
				t.Fatalf("detectConfuserExStringDecrypter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfuserExDecrypterRequiresInitializerFlow(t *testing.T) {
	code := appendIL(nil, il{0x7e, 0x04000002}, il{0x7e, 0x04000003}, il{0x6f, 0x0a000001})
	f := decrypterFixture(code, 2)
	// Merely mentioning an RVA token somewhere in a method which also writes
	// the candidate's array does not establish that the RVA supplies that array.
	init := appendIL(nil, il{0xd0, 0x04000001})
	init = append(init, 0x26, 0x14)
	init = appendIL(init, il{0x80, 0x04000002})
	writeFixtureMethod(f.Header, 256, append(init, 0x2a))
	if detectConfuserExStringDecrypter(f) {
		t.Fatal("accepted unrelated RVA token and array assignment in same method")
	}
}

func TestConfuserExDecrypterRejectsCosmoteerGetName(t *testing.T) {
	// Exact 106-byte IL of Cosmoteer.Data.ID<T>.GetName(int), token
	// 0x06005882, statically read from the reported Cosmoteer.dll sample.
	// Three ldsfld and three callvirt instructions fooled the old byte tally.
	code, err := hex.DecodeString("022d067e0803000a2a7e083f000a6f093f000a0a7e0b3f000a026f0e3f000a0bde462672b2e10270d06d02001b280902000a252d0426142b056f4202000a284f02000a282402000a72c0e102700f00286102000a284f02000a282402000afe1a1200280d3f000adc072a")
	if err != nil {
		t.Fatal(err)
	}
	if detectConfuserExStringDecrypter(decrypterFixture(code, 2)) {
		t.Fatal("ordinary generic dictionary lookup classified as ConfuserEx")
	}
}

func TestConfuserExDecrypterMalformedIL(t *testing.T) {
	valid := appendIL(nil, il{0x7e, 0x04000002}, il{0x7e, 0x04000003}, il{0x6f, 0x0a000001})
	for _, tail := range [][]byte{{0x7e}, {0xfe}, {0xfe, 0xff}, {0x45, 0xff, 0xff, 0xff, 0xff}} {
		code := append(make([]byte, 40), valid...)
		code = append(code, tail...)
		f := decrypterFixture(code, 2)
		if detectConfuserExStringDecrypter(f) {
			t.Errorf("accepted malformed IL tail %x", tail)
		}
	}
	f := decrypterFixture(valid, 2)
	methods, ok := f.CLR.MetadataTables[peparser.MethodDef].Content.([]peparser.MethodDefTableRow)
	if !ok {
		t.Fatal("fixture MethodDef table has unexpected row type")
	}
	methods[0].RVA = ^uint32(0)
	if detectConfuserExStringDecrypter(f) {
		t.Error("accepted out-of-range method RVA")
	}
}

// The fixture models the original detector's lazy-delegate shape, with a
// separate initializer linking a real RVA blob to the loaded static byte array.
func decrypterFixture(code []byte, initializedField uint32) *peparser.File {
	f := &peparser.File{Header: make([]byte, 1024)}
	f.HasCLR = true
	f.CLR.MetadataStreams = map[string][]byte{
		"#Blob":    {0, 4, 0, 1, 0x0e, 8, 3, 6, 0x1d, 5, 2, 6, 0x1c},
		"#Strings": []byte("\x00InitializeArray\x00RuntimeHelpers\x00System.Runtime.CompilerServices\x00"),
	}
	f.CLR.MetadataTables = map[int]*peparser.MetadataTable{
		peparser.FieldRVA:  {Content: []peparser.FieldRVATableRow{{RVA: 800, Field: 1}}},
		peparser.Field:     {Content: []peparser.FieldTableRow{{}, {Signature: 6}, {Signature: 10}, {Signature: 6}}},
		peparser.MethodDef: {Content: []peparser.MethodDefTableRow{{RVA: 64, Flags: methodAttrStatic, Signature: 1}, {RVA: 256, Flags: methodAttrStatic}}},
		peparser.MemberRef: {Content: []peparser.MemberRefTableRow{{}, {Class: 9, Name: 1}}},
		peparser.TypeRef:   {Content: []peparser.TypeRefTableRow{{TypeName: 17, TypeNamespace: 32}}},
	}
	if len(code) < 40 {
		code = append(code, make([]byte, 40-len(code))...)
		code = append(code, 0x2a)
	}
	writeFixtureMethod(f.Header, 64, code)
	init := []byte{0x1a}
	init = appendIL(init, il{0x8d, 0x01000002})
	init = append(init, 0x25)
	init = appendIL(init, il{0xd0, 0x04000001}, il{0x28, 0x0a000002}, il{0x80, 0x04000000 | initializedField})
	writeFixtureMethod(f.Header, 256, append(init, 0x2a))
	return f
}

func writeFixtureMethod(data []byte, rva int, code []byte) {
	data[rva] = 3
	data[rva+1] = 0x30
	binary.LittleEndian.PutUint32(data[rva+4:], uint32(len(code))) //nolint:gosec // fixture bodies are a few dozen bytes
	copy(data[rva+12:], code)
}

// il is one fixture instruction: a one-byte opcode with a 4-byte operand.
type il struct {
	op  byte
	arg uint32
}

func appendIL(code []byte, ins ...il) []byte {
	for _, in := range ins {
		code = append(code, in.op)
		code = binary.LittleEndian.AppendUint32(code, in.arg)
	}
	return code
}
