package main

import (
	"fmt"
	"math/rand"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func GetRandomString() []byte {
	const charset = "abcdefghijklmnopqrstuvwxyz"
	rando := rand.New(rand.NewSource(time.Now().UnixNano()))
	res := make([]byte, 10)
	for i := range res {
		res[i] = charset[rando.Intn(len(charset))]
	}
	return res
}

func main() {
	hash, err := bcrypt.GenerateFromPassword(GetRandomString(), 15)
	if err != nil {
		panic(err)
	}
	fmt.Println("hash: ", hash)
}
