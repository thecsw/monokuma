package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"

	"github.com/thecsw/rei"
)

var (
	// URLRegexp is a regular expression to match URLs.
	URLRegexp = regexp.MustCompile(`(https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()!@:%_\+.~#?&\/\/=]*))`)

	// keysize is used to generate random string.
	keysize *int

	// alphabet is used to generate random string.
	alphabet *string
)

// gen generates random string of given length (keysize) from alphabet.
func gen() string {
	b := make([]byte, 2)
	res := make([]rune, *keysize)
	for i := range res {
		rand.Read(b) // will read 2bytes=16bits=2^16=65535val
		res[i] = rune((*alphabet)[uint16(binary.BigEndian.Uint16(b)%uint16(len(*alphabet)))])
	}
	return string(res)
}

// ensureFileExists checks if file exists, exits if not.
func ensureFileExists(path, description string) {
	if !rei.FileMustExist(path) {
		fmt.Printf("%s '%s' does not exist\n", description, path)
		os.Exit(1)
	}
}

// getEnvOrExit gets environment variable or exits if it's not set.
func getEnvOrExit(envname string, target *string) {
	*target = os.Getenv(envname)
	if *target == "" {
		fmt.Printf("environment variable '%s' is not set\n", envname)
		os.Exit(1)
	}
}
