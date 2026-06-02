package recon

import (
	"unicode/utf16"

	peparser "github.com/saferwall/pe"
)

// CountPUAUserStrings parses a managed PE and returns how many entries in its
// #US (user-string) heap are dominated by Private-Use-Area characters
// (U+E000..U+F8FF), the \ueXXXX strings ConfuserEx string-encryption leaves
// behind when decryption did not run. Returns (count, total, nil) on success.
// A non-nil error means the file could not be parsed as a managed PE (the
// caller treats that as "cannot verify", not "failed").
//
// Note: some ConfuserEx configs store encrypted literals decoded at runtime
// (no PUA in #US); for those this returns 0 and verification relies on the
// post-decompile source scan instead.
func CountPUAUserStrings(path string) (pua int, total int, err error) {
	f, err := peparser.New(path, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	if err := f.Parse(); err != nil {
		return 0, 0, err
	}
	us := f.CLR.MetadataStreams["#US"]
	if len(us) == 0 {
		return 0, 0, nil
	}

	// #US layout (ECMA-335 II.24.2.4): a leading empty blob, then a sequence of
	// [compressed-length][UTF-16LE bytes][1 trailing flag byte] entries.
	p := 1
	for p < len(us) {
		b0 := us[p]
		var ln, hp int
		switch {
		case b0&0x80 == 0:
			ln, hp = int(b0), p+1
		case b0&0xc0 == 0x80:
			if p+1 >= len(us) {
				break
			}
			ln, hp = int(b0&0x3f)<<8|int(us[p+1]), p+2
		default:
			if p+3 >= len(us) {
				break
			}
			ln, hp = int(b0&0x1f)<<24|int(us[p+1])<<16|int(us[p+2])<<8|int(us[p+3]), p+4
		}
		if ln <= 0 || hp+ln > len(us) {
			break
		}
		// Drop the trailing flag byte; the rest is UTF-16LE.
		raw := us[hp : hp+ln-1]
		if s := decodeUTF16LE(raw); s != "" {
			total++
			if puaCharDensity(s) > 0.5 {
				pua++
			}
		}
		p = hp + ln
	}
	return pua, total, nil
}

// decodeUTF16LE decodes a little-endian UTF-16 byte slice to a Go string.
func decodeUTF16LE(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}
