// The rautelap tool prints a quote from a famous bending unit.
package main

import (
	"bufio"
	"io"
	"os"
)

func main() {

	file, err := os.Create("/tmp/hubertJfarnsworth")

	if err != nil {
		panic("Error opening file")
	}

	w := bufio.NewWriter(file)
	io.WriteString(w, "Bite my shiny metal A**")

	err = w.Flush()

	if err != nil {
		println("Check file, some data maybe missing")
	}

	file.Close()
}
