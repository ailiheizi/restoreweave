package main

import (
	"os"
)

func main() {
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(root.Code())
}
