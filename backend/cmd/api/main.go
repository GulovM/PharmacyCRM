package main

import (
	"fmt"
	"os"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
)

func main() {
	if _, err := config.Load(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
