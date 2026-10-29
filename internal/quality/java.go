package quality

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/UberMorgott/morgue/internal/runmodel"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Java struct{}

func init() { Register("java", Java{}) }
func (Java) Collect(ctx context.Context, in Input) (runmodel.QualityReport, error) {
	r := emptyFamily("java")
	inv, err := JavaInventory(ctx, in.Target.InputPath, nil)
	if err == nil {
		r.Functions.Found = countMetric(inv.Methods, "measured", fmt.Sprintf("JVM method_info with Code attribute; excluded %d abstract/native/no-code methods; constructors and synthetic bodies included", inv.ExcludedMethods), nil)
		r.Types.Found = countMetric(inv.Types, "measured", "Class-file tables; archive entries including multi-release variants counted separately", nil)
	} else {
		r.Warnings = append(r.Warnings, "Bytecode inventory unavailable: "+err.Error())
	}
	if err := collectSource(ctx, in, ".java", &r); err != nil {
		return r, err
	}
	collectInvocationDiagnostics(in, "vineflower", &r)
	finishFamily(&r)
	return r, nil
}

// JavaInventory streams class files without extracting or executing input code.
func JavaInventory(ctx context.Context, path string, visit func(module string, id Identity) error) (Inventory, error) {
	var inv Inventory
	one := func(name string, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var fn func(Identity) error
		if visit != nil {
			fn = func(id Identity) error { return visit(name, id) }
		}
		v, err := readClass(r, fn)
		inv.Types += v.Types
		inv.Methods += v.Methods
		inv.ExcludedMethods += v.ExcludedMethods
		return err
	}
	if strings.EqualFold(filepath.Ext(path), ".class") {
		f, err := os.Open(path)
		if err != nil {
			return inv, err
		}
		defer f.Close()
		return invAfter(&inv, func() error { return one(filepath.Base(path), f) })
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return inv, err
	}
	defer z.Close()
	for _, entry := range z.File {
		if !strings.HasSuffix(entry.Name, ".class") {
			continue
		}
		r, e := entry.Open()
		if e != nil {
			return inv, e
		}
		e = one(entry.Name, r)
		ce := r.Close()
		if e != nil {
			return inv, fmt.Errorf("%s: %w", entry.Name, e)
		}
		if ce != nil {
			return inv, ce
		}
	}
	return inv, nil
}
func invAfter(inv *Inventory, fn func() error) (Inventory, error) { err := fn(); return *inv, err }
func readClass(src io.Reader, visit func(Identity) error) (Inventory, error) {
	var inv Inventory
	// Bound each untrusted class read; attribute bodies are streamed, not allocated.
	r := io.LimitReader(src, 128*1024*1024)
	read := func(v any) error { return binary.Read(r, binary.BigEndian, v) }
	u2 := func() (uint16, error) { var n uint16; e := read(&n); return n, e }
	skip := func(n int64) error { _, e := io.CopyN(io.Discard, r, n); return e }
	var magic uint32
	if err := read(&magic); err != nil {
		return inv, err
	}
	if magic != 0xcafebabe {
		return inv, fmt.Errorf("invalid class magic")
	}
	if err := skip(4); err != nil {
		return inv, err
	}
	n, e := u2()
	if e != nil {
		return inv, e
	}
	if n == 0 {
		return inv, fmt.Errorf("empty constant pool")
	}
	pool := make([]string, int(n))
	classNames := make([]uint16, int(n))
	for i := 1; i < int(n); i++ {
		var tag uint8
		if err := read(&tag); err != nil {
			return inv, err
		}
		var err error
		switch tag {
		case 1:
			var size uint16
			size, err = u2()
			if err == nil {
				b := make([]byte, int(size))
				_, err = io.ReadFull(r, b)
				pool[i] = string(b)
			}
		case 3, 4:
			err = skip(4)
		case 5, 6:
			err = skip(8)
			i++
		case 7:
			classNames[i], err = u2()
		case 8, 16, 19, 20:
			err = skip(2)
		case 9, 10, 11, 12, 17, 18:
			err = skip(4)
		case 15:
			err = skip(3)
		default:
			return inv, fmt.Errorf("unknown constant pool tag %d", tag)
		}
		if err != nil {
			return inv, err
		}
	}
	if err := skip(2); err != nil {
		return inv, err
	}
	this, e := u2()
	if e != nil {
		return inv, e
	}
	name := "unknown"
	if int(this) < len(classNames) && int(classNames[this]) < len(pool) {
		name = pool[classNames[this]]
	}
	if err := skip(2); err != nil {
		return inv, err
	}
	interfaces, e := u2()
	if e != nil {
		return inv, e
	}
	if err := skip(int64(interfaces) * 2); err != nil {
		return inv, err
	}
	attrs := func() (bool, error) {
		n, e := u2()
		if e != nil {
			return false, e
		}
		code := false
		for j := 0; j < int(n); j++ {
			idx, e := u2()
			if e != nil {
				return false, e
			}
			var size uint32
			if err := read(&size); err != nil {
				return false, err
			}
			if int(idx) >= len(pool) {
				return false, fmt.Errorf("attribute name outside constant pool")
			}
			if pool[idx] == "Code" {
				code = true
			}
			if err := skip(int64(size)); err != nil {
				return false, err
			}
		}
		return code, nil
	}
	fields, e := u2()
	if e != nil {
		return inv, e
	}
	for i := 0; i < int(fields); i++ {
		if err := skip(6); err != nil {
			return inv, err
		}
		if _, err := attrs(); err != nil {
			return inv, err
		}
	}
	methods, e := u2()
	if e != nil {
		return inv, e
	}
	for i := 0; i < int(methods); i++ {
		flags, e := u2()
		if e != nil {
			return inv, e
		}
		ni, e := u2()
		if e != nil {
			return inv, e
		}
		di, e := u2()
		if e != nil {
			return inv, e
		}
		if int(ni) >= len(pool) || int(di) >= len(pool) {
			return inv, fmt.Errorf("method identity outside constant pool")
		}
		code, e := attrs()
		if e != nil {
			return inv, e
		}
		if !code || flags&0x500 != 0 {
			inv.ExcludedMethods++
			continue
		}
		inv.Methods++
		if visit != nil {
			if err := visit(Identity{Name: name + "." + pool[ni] + pool[di], Kind: "method"}); err != nil {
				return inv, err
			}
		}
	}
	if _, err := attrs(); err != nil {
		return inv, err
	}
	inv.Types = 1
	if visit != nil {
		if err := visit(Identity{Name: name, Kind: "type"}); err != nil {
			return inv, err
		}
	}
	return inv, nil
}
