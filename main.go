package main

import (
	"os"

	"github.com/burgrp/reg/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
