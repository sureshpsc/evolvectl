package main

import (
	"fmt"
	"os"

	"github.com/sureshpsc/evolvectl/internal/cli"
)

func main() {
	code, err := cli.Execute(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	os.Exit(code)
}
