// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os/exec"
	"testing"
)

func TestMain(t *testing.T) {
	checkOutput(t, "greeting.go", "Hello, Gophers!\n")
}

// checkOutput from running a program against expectations.
func checkOutput(t *testing.T, file, expected string) {
	actual := runCmd(t, file)
	if expected != actual {
		t.Errorf("Expected output %q, got %q.", expected, actual)
	}
}

// runCmd a program and capture the output.
func runCmd(t *testing.T, file string) string {
	out, err := exec.Command("go", "run", file).Output()
	if e, ok := err.(*exec.ExitError); ok {
		t.Fatalf("unsuccessful exit: %s", e.Stderr)
	} else if err != nil {
		t.Fatalf("%s", err)
	}
	return string(out)
}
