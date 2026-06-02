package recon

import (
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
//   - the assembly has at least one FieldRVA row (the encrypted data array),
//   - a STATIC method whose signature is exactly `string(int32)`
//     (calling convention DEFAULT, 1 param, ret ELEMENT_TYPE_STRING, arg I4),
//   - whose tiny body (40..120 bytes) loads >=2 static fields (ldsfld) and
//     makes >=1 callvirt — the lazy-init-delegate-then-invoke shape.
//
// These bounds were tuned against the real sample plus ~2800 clean framework /
// NuGet assemblies: DataLoader.dll matches, ilspycmd.dll and the rest do not.
// A bare `static string(int)` exists in ~10% of clean assemblies (resource-
// string getters), which is why the FieldRVA + body-shape conjunction is
// required — those getters are either branchy/large or do no ldsfld.

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
	if frva := f.CLR.MetadataTables[peparser.FieldRVA]; frva == nil {
		return false
	} else if rows, ok := frva.Content.([]peparser.FieldRVATableRow); !ok || len(rows) == 0 {
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

	for _, m := range rows {
		if m.RVA == 0 || m.Flags&methodAttrStatic == 0 {
			continue
		}
		if !blobSigStartsWith(blob, m.Signature, strDecrypterSig) {
			continue
		}
		if decrypterBodyMatches(f, m.RVA) {
			return true
		}
	}
	return false
}

// blobSigStartsWith reports whether the blob-heap entry at idx (compressed
// length prefix per ECMA-335 II.23.2) begins with want.
func blobSigStartsWith(blob []byte, idx uint32, want []byte) bool {
	if int(idx) >= len(blob) {
		return false
	}
	b0 := blob[idx]
	var p int
	switch {
	case b0&0x80 == 0:
		p = int(idx) + 1
	case b0&0xc0 == 0x80:
		p = int(idx) + 2
	default:
		p = int(idx) + 4
	}
	if p+len(want) > len(blob) {
		return false
	}
	for i, w := range want {
		if blob[p+i] != w {
			return false
		}
	}
	return true
}

// decrypterBodyMatches reads the method IL at rva and checks the ConfuserEx
// string-decrypter stub shape: small body that loads >=2 static fields and
// issues >=1 callvirt. Opcodes: ldsfld=0x7e, callvirt=0x6f.
//
// This is a coarse opcode tally (operand bytes can coincidentally equal an
// opcode value), which is acceptable here: it only refines an already-narrow
// candidate (static string(int32) + FieldRVA), and the bounds were validated to
// not false-positive across ~2800 clean assemblies.
func decrypterBodyMatches(f *peparser.File, rva uint32) bool {
	// Read the method header first byte to learn body size (tiny vs fat header,
	// ECMA-335 II.25.4). Read a small window, then the body.
	hdr, err := f.GetData(rva, 12)
	if err != nil || len(hdr) == 0 {
		return false
	}
	var codeSize uint32
	var codeStart uint32
	if hdr[0]&0x03 == 0x02 { // tiny header: size in high 6 bits
		codeSize = uint32(hdr[0] >> 2)
		codeStart = rva + 1
	} else { // fat header: 12 bytes, CodeSize at offset 4
		if len(hdr) < 8 {
			return false
		}
		codeSize = uint32(hdr[4]) | uint32(hdr[5])<<8 | uint32(hdr[6])<<16 | uint32(hdr[7])<<24
		codeStart = rva + 12
	}
	if codeSize < decrypterBodyMin || codeSize > decrypterBodyMax {
		return false
	}
	code, err := f.GetData(codeStart, codeSize)
	if err != nil || uint32(len(code)) < codeSize {
		return false
	}
	ldsfld, callvirt := 0, 0
	for _, b := range code {
		switch b {
		case 0x7e:
			ldsfld++
		case 0x6f:
			callvirt++
		}
	}
	return ldsfld >= decrypterMinLdsfld && callvirt >= decrypterMinCallvt
}
