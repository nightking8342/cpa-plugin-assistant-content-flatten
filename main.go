// Package main builds the `assistant-content-flatten` CLIProxyAPI plugin.
//
// CPA loads plugins as in-process dynamic libraries. abi.go holds the cgo
// bridge that CPA calls into; this file only exists because -buildmode=c-shared
// requires a main package with a main function.
package main

func main() {}
