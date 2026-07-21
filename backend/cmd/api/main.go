package main

import (
	"fmt"
	"os"

	"github.com/GulovM/PharmacyCRM/backend/internal/bootstrap"
)

func main() {
	if err := bootstrap.RunAPI(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
