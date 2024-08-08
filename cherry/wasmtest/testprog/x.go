package main

import (
	"runtime"
	"runtime/debug"
)

func init() {
	println("init function called")
}

var ch = make(chan float64)

//go:wasmexport E
func E(a int64, b int32, c float64, d float32) { // various types of args, no result
	println("=== E ===")
	// goroutine
	go func() { ch <- float64(a) + float64(b) + c + float64(d) + 100 }()
	debug.PrintStack() // traceback
	grow([100]int{10}) // stack growth
	runtime.GC()       // GC
	println("=== E end ===")
}

//go:wasmexport F
func F() int64 { // no arg, has result
	f := int64(<-ch * 100) // force a goroutine switch
	println("F =", f)
	return f
}

//go:wasmexport G
func G(x int32) {
	println("G", x)
	if x%2 == 0 {
		G(x - 1) // simple recursion within this module
	} else {
		J(x - 1) // mutual recursion between host and this module
	}
	println("G", x, "end")
}

//go:wasmimport test I
func I() int64

//go:wasmimport test J
func J(int32)

func main() {
	println("hello")
	println("main: I =", I())
}

func grow(x [100]int) {
	if x[0] == 0 {
		println("=== grow ===")
		debug.PrintStack()
		return
	}
	x[0]--
	grow(x)
}
