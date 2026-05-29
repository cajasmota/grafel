// +build android ios

package mylib

import (
	"golang.org/x/mobile/bind"
)

var _ = bind.Seq{}

// Greet is exported across the FFI boundary to Java/ObjC.
func Greet(name string) string {
	return "Hello, " + name
}

// Add is another bound function.
func Add(a, b int) int {
	return a + b
}

// helper is unexported and must not be treated as a bound func.
func helper() {}
