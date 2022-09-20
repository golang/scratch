// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This programs does ad-hoc code signing fo Mach-O files.
// It tries to do what darwin linker does.

package main

import (
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unsafe"
)

const (
	pageSizeBits = 12
	pageSize     = 1 << pageSizeBits
)

const LC_CODE_SIGNATURE = 0x1d

const fileHeaderSize64 = 8 * 4

const (
	CSMAGIC_REQUIREMENT        = 0xfade0c00 // single Requirement blob
	CSMAGIC_REQUIREMENTS       = 0xfade0c01 // Requirements vector (internal requirements)
	CSMAGIC_CODEDIRECTORY      = 0xfade0c02 // CodeDirectory blob
	CSMAGIC_EMBEDDED_SIGNATURE = 0xfade0cc0 // embedded form of signature data
	CSMAGIC_DETACHED_SIGNATURE = 0xfade0cc1 // multi-arch collection of embedded signatures

	CSSLOT_CODEDIRECTORY = 0 // slot index for CodeDirectory
)

const (
	kSecCodeSignatureNoHash              = 0 // null value
	kSecCodeSignatureHashSHA1            = 1 // SHA-1
	kSecCodeSignatureHashSHA256          = 2 // SHA-256
	kSecCodeSignatureHashSHA256Truncated = 3 // SHA-256 truncated to first 20 bytes
	kSecCodeSignatureHashSHA384          = 4 // SHA-384
	kSecCodeSignatureHashSHA512          = 5 // SHA-512
)

const (
	CS_EXECSEG_MAIN_BINARY     = 0x1   // executable segment denotes main binary
	CS_EXECSEG_ALLOW_UNSIGNED  = 0x10  // allow unsigned pages (for debugging)
	CS_EXECSEG_DEBUGGER        = 0x20  // main binary is debugger
	CS_EXECSEG_JIT             = 0x40  // JIT enabled
	CS_EXECSEG_SKIP_LV         = 0x80  // skip library validation
	CS_EXECSEG_CAN_LOAD_CDHASH = 0x100 // can bless cdhash for execution
	CS_EXECSEG_CAN_EXEC_CDHASH = 0x200 // can execute blessed cdhash
)

type Blob struct {
	typ    uint32 // type of entry
	offset uint32 // offset of entry
	// data follows
}

func (b *Blob) put(out []byte) []byte {
	out = put32be(out, b.typ)
	out = put32be(out, b.offset)
	return out
}

type SuperBlob struct {
	magic  uint32 // magic number
	length uint32 // total length of SuperBlob
	count  uint32 // number of index entries following
	// blobs []Blob
}

func (s *SuperBlob) put(out []byte) []byte {
	out = put32be(out, s.magic)
	out = put32be(out, s.length)
	out = put32be(out, s.count)
	return out
}

type CodeDirectory struct {
	magic         uint32 // magic number (CSMAGIC_CODEDIRECTORY)
	length        uint32 // total length of CodeDirectory blob
	version       uint32 // compatibility version
	flags         uint32 // setup and mode flags
	hashOffset    uint32 // offset of hash slot element at index zero
	identOffset   uint32 // offset of identifier string
	nSpecialSlots uint32 // number of special hash slots
	nCodeSlots    uint32 // number of ordinary (code) hash slots
	codeLimit     uint32 // limit to main image signature range
	hashSize      uint8  // size of each hash in bytes
	hashType      uint8  // type of hash (cdHashType* constants)
	_pad1         uint8  // unused (must be zero)
	pageSize      uint8  // log2(page size in bytes); 0 => infinite
	_pad2         uint32 // unused (must be zero)
	scatterOffset uint32
	teamOffset    uint32
	_pad3         uint32
	codeLimit64   uint64
	execSegBase   uint64
	execSegLimit  uint64
	execSegFlags  uint64
	// data follows
}

func (c *CodeDirectory) put(out []byte) []byte {
	out = put32be(out, c.magic)
	out = put32be(out, c.length)
	out = put32be(out, c.version)
	out = put32be(out, c.flags)
	out = put32be(out, c.hashOffset)
	out = put32be(out, c.identOffset)
	out = put32be(out, c.nSpecialSlots)
	out = put32be(out, c.nCodeSlots)
	out = put32be(out, c.codeLimit)
	out = put8(out, c.hashSize)
	out = put8(out, c.hashType)
	out = put8(out, c._pad1)
	out = put8(out, c.pageSize)
	out = put32be(out, c._pad2)
	out = put32be(out, c.scatterOffset)
	out = put32be(out, c.teamOffset)
	out = put32be(out, c._pad3)
	out = put64be(out, c.codeLimit64)
	out = put64be(out, c.execSegBase)
	out = put64be(out, c.execSegLimit)
	out = put64be(out, c.execSegFlags)
	return out
}

type linkeditDataCmd struct {
	cmd      uint32
	cmdsize  uint32 // sizeof(struct linkedit_data_command)
	dataoff  uint32 // file offset of data in __LINKEDIT segment
	datasize uint32 // file size of data in __LINKEDIT segment
}

func (l *linkeditDataCmd) put(out []byte) []byte {
	// load command is little endian
	out = put32le(out, l.cmd)
	out = put32le(out, l.cmdsize)
	out = put32le(out, l.dataoff)
	out = put32le(out, l.datasize)
	return out
}

func get32le(b []byte) uint32           { return binary.LittleEndian.Uint32(b) }
func put32le(b []byte, x uint32) []byte { binary.LittleEndian.PutUint32(b, x); return b[4:] }
func put32be(b []byte, x uint32) []byte { binary.BigEndian.PutUint32(b, x); return b[4:] }
func put64le(b []byte, x uint64) []byte { binary.LittleEndian.PutUint64(b, x); return b[8:] }
func put64be(b []byte, x uint64) []byte { binary.BigEndian.PutUint64(b, x); return b[8:] }
func put8(b []byte, x uint8) []byte     { b[0] = x; return b[1:] }
func puts(b, s []byte) []byte           { n := copy(b, s); return b[n:] }

// round x up to a multiple of n. n must be a power of 2.
func roundUp(x, n int) int { return (x + n - 1) &^ (n - 1) }

const verbose = false

func main() {
	if len(os.Args) != 2 {
		fmt.Println("usage: codesign <binary>")
		os.Exit(1)
	}

	fname := os.Args[1]
	f, err := os.OpenFile(fname, os.O_RDWR, 0)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	mf, err := macho.NewFile(f)
	if err != nil {
		panic(err)
	}
	if mf.Magic != macho.Magic64 {
		panic("not 64-bit")
	}
	if mf.ByteOrder != binary.LittleEndian {
		panic("not little endian")
	}

	// find existing LC_CODE_SIGNATURE and __LINKEDIT segment
	var sigOff, sigSz, linkeditOff int
	var linkeditSeg, textSeg *macho.Segment
	loadOff := fileHeaderSize64
	for _, l := range mf.Loads {
		data := l.Raw()
		cmd, sz := get32le(data), get32le(data[4:])
		if cmd == LC_CODE_SIGNATURE {
			sigOff = int(get32le(data[8:]))
			sigSz = int(get32le(data[12:]))
		}
		if seg, ok := l.(*macho.Segment); ok {
			switch seg.Name {
			case "__LINKEDIT":
				linkeditSeg = seg
				linkeditOff = loadOff
			case "__TEXT":
				textSeg = seg
			}
		}
		loadOff += int(sz)
	}

	if sigOff == 0 {
		st, err := f.Stat()
		if err != nil {
			panic(err)
		}
		sigOff = int(st.Size())
		sigOff = roundUp(sigOff, 16) // round up to 16 bytes ???
		err = f.Truncate(int64(sigOff))
		if err != nil {
			panic(err)
		}
	}

	// compute sizes
	id := "a.out\000"
	nhashes := (sigOff + pageSize - 1) / pageSize
	idOff := int(unsafe.Sizeof(CodeDirectory{}))
	hashOff := idOff + len(id)
	cdirSz := hashOff + nhashes*sha256.Size
	sz := int(unsafe.Sizeof(SuperBlob{})+unsafe.Sizeof(Blob{})) + cdirSz
	if sigSz != 0 && sz != sigSz {
		println(sz, sigSz)
		panic("LC_CODE_SIGNATURE exists but with a different size. already signed?")
	}

	if sigSz == 0 { // LC_CODE_SIGNATURE does not exist. Add one.
		csCmdSz := int(unsafe.Sizeof(linkeditDataCmd{}))
		csCmd := linkeditDataCmd{
			cmd:      LC_CODE_SIGNATURE,
			cmdsize:  uint32(csCmdSz),
			dataoff:  uint32(sigOff),
			datasize: uint32(sz),
		}
		if loadOff+csCmdSz > int(mf.Sections[0].Offset) {
			panic("no space for adding LC_CODE_SIGNATURE")
		}
		out := make([]byte, csCmdSz)
		csCmd.put(out)
		_, err = f.WriteAt(out, int64(loadOff))
		if err != nil {
			panic(err)
		}

		// fix up header: update Ncmd and Cmdsz
		var tmp [8]byte
		put32le(tmp[:4], mf.FileHeader.Ncmd+1)
		_, err = f.WriteAt(tmp[:4], int64(unsafe.Offsetof(mf.FileHeader.Ncmd)))
		if err != nil {
			panic(err)
		}
		put32le(tmp[:4], mf.FileHeader.Cmdsz+uint32(csCmdSz))
		_, err = f.WriteAt(tmp[:4], int64(unsafe.Offsetof(mf.FileHeader.Cmdsz)))
		if err != nil {
			panic(err)
		}

		// fix up LINKEDIT segment: update Memsz and Filesz
		segSz := sigOff + sz - int(linkeditSeg.Offset)
		put64le(tmp[:8], uint64(roundUp(segSz, 0x4000))) // round up to physical page size
		_, err = f.WriteAt(tmp[:8], int64(linkeditOff)+int64(unsafe.Offsetof(macho.Segment64{}.Memsz)))
		if err != nil {
			panic(err)
		}
		put64le(tmp[:8], uint64(segSz))
		_, err = f.WriteAt(tmp[:8], int64(linkeditOff)+int64(unsafe.Offsetof(macho.Segment64{}.Filesz)))
		if err != nil {
			panic(err)
		}
	}

	// emit blob headers
	sb := SuperBlob{
		magic:  CSMAGIC_EMBEDDED_SIGNATURE,
		length: uint32(sz),
		count:  1,
	}
	blob := Blob{
		typ:    CSSLOT_CODEDIRECTORY,
		offset: uint32(unsafe.Sizeof(SuperBlob{}) + unsafe.Sizeof(Blob{})),
	}
	cdir := CodeDirectory{
		magic:        CSMAGIC_CODEDIRECTORY,
		length:       uint32(sz) - uint32(unsafe.Sizeof(SuperBlob{})+unsafe.Sizeof(Blob{})),
		version:      0x20400,
		flags:        0x20002, // adhoc | linkerSigned
		hashOffset:   uint32(hashOff),
		identOffset:  uint32(idOff),
		nCodeSlots:   uint32(nhashes),
		codeLimit:    uint32(sigOff),
		hashSize:     sha256.Size,
		hashType:     kSecCodeSignatureHashSHA256,
		pageSize:     uint8(pageSizeBits),
		execSegBase:  textSeg.Offset,
		execSegLimit: textSeg.Filesz,
	}
	if mf.Type == macho.TypeExec {
		cdir.execSegFlags = CS_EXECSEG_MAIN_BINARY
	}

	out := make([]byte, sz)
	outp := out

	outp = sb.put(outp)
	outp = blob.put(outp)
	outp = cdir.put(outp)
	outp = puts(outp, []byte(id))

	// emit hashes
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		panic(err)
	}
	var buf [pageSize]byte
	fileOff := 0
	for fileOff < sigOff {
		n, err := io.ReadFull(f, buf[:])
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			panic(err)
		}
		if fileOff+n > sigOff {
			n = sigOff - fileOff
		}
		h := sha256.New()
		h.Write(buf[:n])
		b := h.Sum(nil)
		outp = puts(outp, b[:])
		fileOff += n
	}

	if verbose {
		for i := 0; i < len(out); i += 16 {
			end := i + 16
			if end > len(out) {
				end = len(out)
			}
			fmt.Printf("% x\n", out[i:end])
		}
	}

	_, err = f.WriteAt(out, int64(sigOff))
	if err != nil {
		panic(err)
	}
}
