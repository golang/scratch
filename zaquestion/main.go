// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"github.com/adamryman/gophersay/gopher"
)

func main() {
	fmt.Println("Learning to contribute to your favorite language at Gophercon 2018 is rad!")
	fmt.Printf("Heres a proverb:\n\n")
	gopher.Proverb(os.Stdout)
}
