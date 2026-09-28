//go:build !windows

package main

import "fmt"

func main() { fmt.Println("On Linux, Sockt installs and restarts itself without a companion updater.") }
