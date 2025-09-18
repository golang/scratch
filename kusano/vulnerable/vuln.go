package main

import "golang.org/x/text/language"

func Vulnerable() {
	language.Parse("")
}
