// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The cmcguinness tool displays a random number.
package main

import "fmt"

// randomNumber implementation sourced from https://xkcd.com/221/.
// chosen by a fair dice roll.
// guaranteed to be random
const randomNumber = 4

func main() {
	fmt.Printf("Your random number of the day is: %v", randomNumber)
}
