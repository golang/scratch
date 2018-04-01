// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
	mrand "math/rand"
)

var quotes = []string{
	"This here’s a gun powder activated, 27 caliber, full auto, no kickback, nail-throwing mayhem man",
	"You come at the king, you best not miss.",
	"A life. A life, Jimmy, you know what that is? It's the stuff that happens while you're waiting for moments that never come.",
}

func main() {
	n, err := rand.Int(rand.Reader, big.NewInt(2<<32-1))
	if err != nil {
		panic(err)
	}
	r := mrand.New(mrand.NewSource(n.Int64()))
	choice := r.Intn(len(quotes))
	fmt.Println(quotes[choice])
}
