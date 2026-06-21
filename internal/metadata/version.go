// Package metadata reads Unity IL2CPP global-metadata.dat headers natively.
package metadata

import (
	"encoding/binary"
	"fmt"
	"os"
)

// MetadataMagic is the little-endian sanity value at offset 0 of global-metadata.dat.
const MetadataMagic uint32 = 0xFAB11BAF

// ReadVersion validates the magic at offset 0 and returns the int32 version at
// offset 4 of a global-metadata.dat file.
func ReadVersion(path string) (int32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var head [8]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return 0, fmt.Errorf("read metadata header %s: %w", path, err)
	}
	magic := binary.LittleEndian.Uint32(head[0:4])
	if magic != MetadataMagic {
		return 0, fmt.Errorf("invalid global-metadata.dat magic: got %#08x, want %#08x", magic, MetadataMagic)
	}
	version := int32(binary.LittleEndian.Uint32(head[4:8]))
	return version, nil
}
