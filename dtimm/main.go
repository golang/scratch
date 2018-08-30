// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// dtimm command hosts a friendly message on port :8080.
package main

import (
	"io"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Hello from GopherCon 2018!")
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
