package recon

import (
	"encoding/binary"

	peparser "github.com/saferwall/pe"
)

// ConfuserEx string-decryptor signature detection.
//
// Some ConfuserEx builds strip every textual marker AND skip the renamer
// (DataLoader.dll: readable type names, no ConfusedByAttribute, normal CLR
// header), so neither the string-marker scan nor the mangled-name ratio in
// heuristics.go fires. What DOES survive is ConfuserEx's string-encryption
// scaffolding: a static `string Decrypt(int)` stub that lazily initialises a
// decrypter from a FieldRVA-backed byte array (<PrivateImplementationDetails>)
// and forwards every literal through it.
//
// We fingerprint that stub directly from metadata + IL:
//   - an RVA-backed data blob initializes a byte[] field read by the stub,
//   - a STATIC method whose signature is exactly `string(int32)`
//     (calling convention DEFAULT, 1 param, ret ELEMENT_TYPE_STRING, arg I4),
//   - whose tiny body (40..120 bytes) loads >=2 static fields (ldsfld) and
//     makes >=1 callvirt — the lazy-init-delegate-then-invoke shape.
//
// An assembly-wide FieldRVA plus opcode counts is insufficient: ordinary
// dictionary getters (Cosmoteer's ID<T>.GetName) also have that shape. Decode
// operands and require a connection to the data initializer instead.

// Static method attribute flag (CorMethodAttr.mdStatic), ECMA-335 II.23.1.10.
const methodAttrStatic = 0x0010

// strDecrypterSig is a MethodDef signature for `static string(int32)`:
// 0x00 = DEFAULT calling convention (no HASTHIS), 0x01 = one parameter,
// 0x0e = ELEMENT_TYPE_STRING (return), 0x08 = ELEMENT_TYPE_I4 (the arg).
var strDecrypterSig = []byte{0x00, 0x01, 0x0e, 0x08}

// IL body-shape bounds for the ConfuserEx string-decrypter stub.
const (
	decrypterBodyMin   = 40
	decrypterBodyMax   = 120
	decrypterMinLdsfld = 2
	decrypterMinCallvt = 1
)

// detectConfuserExStringDecrypter reports whether the managed PE carries a
// ConfuserEx string-encryption decrypter stub. Conservative: requires a
// FieldRVA-backed data array plus a static string(int32) stub with the
// lazy-init body shape. Returns false on any parse gap rather than guessing.
func detectConfuserExStringDecrypter(f *peparser.File) (matched bool) {
	// saferwall's GetData can panic on a malformed/crafted managed PE: when a
	// method-body RVA resolves outside every section it slices the backing array
	// unclamped (helper.go). recon runs in classifyTarget, OUTSIDE the pipeline's
	// per-file recover, so a panic here crashes the whole run. Contain it and
	// treat an unparseable PE as "not a ConfuserEx stub" rather than guessing.
	defer func() {
		if recover() != nil {
			matched = false
		}
	}()
	if f == nil || !f.HasCLR {
		return false
	}

	// Need the encrypted-data array (FieldRVA). Clean libraries can also have
	// FieldRVA (static array initialisers), so this alone is not enough — it is
	// one required conjunct.
	frva := f.CLR.MetadataTables[peparser.FieldRVA]
	if frva == nil {
		return false
	}
	dataRows, ok := frva.Content.([]peparser.FieldRVATableRow)
	if !ok || len(dataRows) == 0 {
		return false
	}

	blob := f.CLR.MetadataStreams["#Blob"]
	mdTbl := f.CLR.MetadataTables[peparser.MethodDef]
	if len(blob) == 0 || mdTbl == nil {
		return false
	}
	rows, ok := mdTbl.Content.([]peparser.MethodDefTableRow)
	if !ok {
		return false
	}
	fieldsTable := f.CLR.MetadataTables[peparser.Field]
	if fieldsTable == nil {
		return false
	}
	fields, ok := fieldsTable.Content.([]peparser.FieldTableRow)
	if !ok {
		return false
	}
	isByteArray := func(token uint32) bool {
		index := token & 0x00ffffff
		return token>>24 == 4 && index > 0 && index <= uint32(len(fields)) &&
			blobSigStartsWith(blob, fields[index-1].Signature, []byte{0x06, 0x1d, 0x05})
	}
	loadedArrays := make(map[uint32]bool)

	for _, m := range rows {
		if m.RVA == 0 || m.Flags&methodAttrStatic == 0 {
			continue
		}
		if !blobSigStartsWith(blob, m.Signature, strDecrypterSig) {
			continue
		}
		code, ok := methodIL(f, m.RVA, decrypterBodyMax)
		if !ok || len(code) < decrypterBodyMin {
			continue
		}
		instructions, ok := decodeIL(code)
		if !ok {
			continue
		}
		ldsfld, callvirt := 0, 0
		for _, ins := range instructions {
			if ins.op == 0x7e {
				ldsfld++
			} else if ins.op == 0x6f {
				callvirt++
			}
		}
		if ldsfld < decrypterMinLdsfld || callvirt < decrypterMinCallvt {
			continue
		}
		for _, ins := range instructions {
			if ins.op == 0x7e && isByteArray(ins.operand) {
				loadedArrays[ins.operand] = true
			}
		}
	}
	if len(loadedArrays) == 0 {
		return false
	}
	dataFields := make(map[uint32]bool)
	for _, data := range dataRows {
		if data.RVA != 0 && data.Field > 0 && data.Field <= uint32(len(fields)) {
			dataFields[0x04000000|data.Field] = true
		}
	}
	// The initializer can be the stub itself, a helper, or the owning type's
	// .cctor. Link it through the exact field token, not merely its presence
	// elsewhere in the assembly. Cap individual bodies to bound malformed input.
	for _, m := range rows {
		code, ok := methodIL(f, m.RVA, 64*1024)
		if !ok {
			continue
		}
		instructions, ok := decodeIL(code)
		if !ok {
			continue
		}
		// dup; ldtoken <RVA field>; call RuntimeHelpers.InitializeArray;
		// stsfld <loaded byte[]> ties the initialized value to this exact array.
		// Co-occurrence of unrelated loads/stores in an initializer is not proof.
		for i := 3; i < len(instructions); i++ {
			if instructions[i-3].op == 0x25 && instructions[i-2].op == 0xd0 &&
				dataFields[instructions[i-2].operand] && instructions[i-1].op == 0x28 &&
				isInitializeArray(f, instructions[i-1].operand) &&
				instructions[i].op == 0x80 && loadedArrays[instructions[i].operand] {
				return true
			}
		}
	}
	return false
}

func isInitializeArray(f *peparser.File, token uint32) bool {
	index := token & 0x00ffffff
	members := f.CLR.MetadataTables[peparser.MemberRef]
	types := f.CLR.MetadataTables[peparser.TypeRef]
	if token>>24 != 0x0a || index == 0 || members == nil || types == nil {
		return false
	}
	memberRows, ok := members.Content.([]peparser.MemberRefTableRow)
	if !ok || index > uint32(len(memberRows)) {
		return false
	}
	member := memberRows[index-1]
	// MemberRefParent uses three tag bits; 1 is TypeRef (ECMA-335 II.24.2.6).
	typeIndex := member.Class >> 3
	if member.Class&7 != 1 || typeIndex == 0 {
		return false
	}
	typeRows, ok := types.Content.([]peparser.TypeRefTableRow)
	if !ok || typeIndex > uint32(len(typeRows)) {
		return false
	}
	typ := typeRows[typeIndex-1]
	heap := f.CLR.MetadataStreams["#Strings"]
	return heapStringAt(heap, member.Name) == "InitializeArray" &&
		heapStringAt(heap, typ.TypeName) == "RuntimeHelpers" &&
		heapStringAt(heap, typ.TypeNamespace) == "System.Runtime.CompilerServices"
}

// blobSigStartsWith reports whether the blob-heap entry at idx (compressed
// length prefix per ECMA-335 II.23.2) begins with want.
func blobSigStartsWith(blob []byte, idx uint32, want []byte) bool {
	if int(idx) >= len(blob) {
		return false
	}
	b0 := blob[idx]
	var p, size int
	switch {
	case b0&0x80 == 0:
		p = int(idx) + 1
		size = int(b0)
	case b0&0xc0 == 0x80:
		p = int(idx) + 2
		if p > len(blob) {
			return false
		}
		size = int(b0&0x3f)<<8 | int(blob[idx+1])
	case b0&0xe0 == 0xc0:
		p = int(idx) + 4
		if p > len(blob) {
			return false
		}
		size = int(b0&0x1f)<<24 | int(blob[idx+1])<<16 | int(blob[idx+2])<<8 | int(blob[idx+3])
	default:
		return false
	}
	if size < len(want) || size > len(blob)-p {
		return false
	}
	for i, w := range want {
		if blob[p+i] != w {
			return false
		}
	}
	return true
}

// methodIL reads a bounded tiny/fat ECMA-335 method body, rejecting malformed
// headers and overflowing RVAs. The caller contains saferwall GetData panics.
func methodIL(f *peparser.File, rva uint32, maxSize uint32) ([]byte, bool) {
	if rva == 0 || rva > ^uint32(0)-12 {
		return nil, false
	}
	// Read the method header first byte to learn body size (tiny vs fat header,
	// ECMA-335 II.25.4). Read a small window, then the body.
	hdr, err := f.GetData(rva, 12)
	if err != nil || len(hdr) == 0 {
		return nil, false
	}
	var codeSize uint32
	var codeStart uint32
	if hdr[0]&0x03 == 0x02 { // tiny header: size in high 6 bits
		codeSize = uint32(hdr[0] >> 2)
		codeStart = rva + 1
	} else if hdr[0]&0x03 == 0x03 { // fat header: size in high nibble of byte 1
		if len(hdr) < 12 {
			return nil, false
		}
		headerSize := uint32(hdr[1]>>4) * 4
		if headerSize < 12 || rva > ^uint32(0)-headerSize {
			return nil, false
		}
		codeSize = binary.LittleEndian.Uint32(hdr[4:8])
		codeStart = rva + headerSize
	} else {
		return nil, false
	}
	if codeSize == 0 || codeSize > maxSize || codeStart > ^uint32(0)-codeSize {
		return nil, false
	}
	code, err := f.GetData(codeStart, codeSize)
	if err != nil || len(code) < int(codeSize) {
		return nil, false
	}
	return code, true
}

type ilInstruction struct {
	op      uint16
	operand uint32
}

// decodeIL walks actual instruction boundaries (ECMA-335 III); operand bytes
// must never be interpreted as opcodes or metadata tokens.
func decodeIL(code []byte) ([]ilInstruction, bool) {
	var instructions []ilInstruction
	for p := 0; p < len(code); {
		op := uint16(code[p])
		p++
		if op == 0xfe {
			if p == len(code) {
				return nil, false
			}
			op = 0xfe00 | uint16(code[p])
			p++
		}
		size := 0
		switch {
		case op == 0x45: // switch: count followed by relative branch targets
			if len(code)-p < 4 {
				return nil, false
			}
			count := binary.LittleEndian.Uint32(code[p:])
			if uint64(count) > uint64((len(code)-p-4)/4) {
				return nil, false
			}
			size = 4 + int(count)*4
		case op >= 0x0e && op <= 0x13 || op == 0x1f || op >= 0x2b && op <= 0x37 || op == 0xde || op == 0xfe12:
			size = 1
		case op >= 0xfe09 && op <= 0xfe0e:
			size = 2
		case op == 0x20 || op == 0x22 || op >= 0x27 && op <= 0x29 || op >= 0x38 && op <= 0x44 ||
			op >= 0x6f && op <= 0x75 || op == 0x79 || op >= 0x7b && op <= 0x81 ||
			op == 0x8c || op == 0x8d || op == 0x8f || op >= 0xa3 && op <= 0xa5 ||
			op == 0xc2 || op == 0xc6 || op == 0xd0 || op == 0xdd || op == 0xfe06 || op == 0xfe07 ||
			op == 0xfe15 || op == 0xfe16 || op == 0xfe1c:
			size = 4
		case op == 0x21 || op == 0x23:
			size = 8
		case op <= 0x0d || op >= 0x14 && op <= 0x1e || op == 0x25 || op == 0x26 || op == 0x2a ||
			op >= 0x46 && op <= 0x6e || op == 0x76 || op == 0x7a || op >= 0x82 && op <= 0x8b ||
			op == 0x8e || op >= 0x90 && op <= 0xa2 || op >= 0xb3 && op <= 0xba || op == 0xc3 ||
			op >= 0xd1 && op <= 0xdc || op == 0xdf || op == 0xe0 || op >= 0xfe00 && op <= 0xfe05 ||
			op == 0xfe0f || op == 0xfe11 || op == 0xfe13 || op == 0xfe14 || op == 0xfe17 ||
			op == 0xfe18 || op == 0xfe1a || op == 0xfe1d || op == 0xfe1e:
		default:
			return nil, false
		}
		if size > len(code)-p {
			return nil, false
		}
		ins := ilInstruction{op: op}
		if size == 4 {
			ins.operand = binary.LittleEndian.Uint32(code[p:])
		}
		instructions = append(instructions, ins)
		p += size
	}
	return instructions, true
}
