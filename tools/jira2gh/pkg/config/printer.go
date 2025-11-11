package config

import (
	"fmt"
	"os"
)

var Quiet bool

func Printf(format string, args ...any) {
	if !Quiet {
		fmt.Printf(format, args...)
	}
}

func Println(args ...any) {
	if !Quiet {
		fmt.Println(args...)
	}
}

func Stderr(format string, args ...any) {
	if !Quiet {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}
