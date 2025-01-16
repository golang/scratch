// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A program for testing wasmexport.
// This is the driver/host program, which provides the imports
// and calls the exports. testprog is the source of the Wasm
// module, which can be compiled to either an executable or a
// library.
//
// To build it as executable:
// GOARCH=wasm GOOS=wasip1 go build -o /tmp/x.wasm ./testprog
//
// To build it as a library:
// GOARCH=wasm GOOS=wasip1 go build -buildmode=c-shared -o /tmp/x.wasm ./testprog
//
// Then run the driver (which works for both modes):
// go run w.go /tmp/x.wasm
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// exported from wasm
var E func(a int64, b int32, c float64, d float32)
var F func() int64
var G func(int32)

func I() int64 {
	println("I start")
	E(20, 3, 0.4, 0.05)
	r := F() * 2
	G(4)
	println("I end =", r)
	return r
}

func J(x int32) {
	println("J", x)
	if x > 0 {
		G(x)
	}
	println("J", x, "end")
}

var errbuf bytes.Buffer
var stderr = io.MultiWriter(os.Stderr, &errbuf)

func main() {
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	// provide import functions from host
	_, err := r.NewHostModuleBuilder("test").
		NewFunctionBuilder().WithFunc(I).Export("I").
		NewFunctionBuilder().WithFunc(J).Export("J").
		Instantiate(ctx)
	if err != nil {
		panic(err)
	}

	buf, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	config := wazero.NewModuleConfig().
		WithStdout(os.Stdout).WithStderr(stderr).
		WithStartFunctions() // don't call _start

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	m, err := r.InstantiateWithConfig(ctx, buf, config)
	if err != nil {
		panic(err)
	}

	// get export functions from the module
	E = func(a int64, b int32, c float64, d float32) {
		exp := m.ExportedFunction("E")
		_, err := exp.Call(ctx, api.EncodeI64(a), api.EncodeI32(b), api.EncodeF64(c), api.EncodeF32(d))
		if err != nil {
			panic(err)
		}
	}
	F = func() int64 {
		exp := m.ExportedFunction("F")
		r, err := exp.Call(ctx)
		if err != nil {
			panic(err)
		}
		rr := int64(r[0])
		println("host: F =", rr)
		return rr
	}
	G = func(x int32) {
		exp := m.ExportedFunction("G")
		_, err := exp.Call(ctx, api.EncodeI32(x))
		if err != nil {
			panic(err)
		}
	}

	entry := m.ExportedFunction("_start")
	if entry != nil {
		// Executable mode.
		fmt.Println("Executable mode: start")
		_, err := entry.Call(ctx)
		fmt.Println(err)
		return
	}

	// Library mode.
	fmt.Println("Libaray mode: call export before initialization")
	shouldPanic(func() { I() })
	// reset module
	m, err = r.InstantiateWithConfig(ctx, buf, config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Library mode: initialize")
	entry = m.ExportedFunction("_initialize")
	_, err = entry.Call(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("\nLibrary mode: call export functions")
	I()
}

func shouldPanic(f func()) {
	defer func() {
		e := recover()
		if e == nil {
			panic("did not panic")
		}
		if !bytes.Contains(errbuf.Bytes(), []byte("runtime: wasmexport function called before runtime initialization")) {
			panic("expected error message missing")
		}
	}()
	f()
}
