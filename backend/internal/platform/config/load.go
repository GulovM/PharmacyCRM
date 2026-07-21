package config

import (
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

func process(groups ...struct {
	prefix string
	target any
}) error {
	for _, group := range groups {
		if err := envconfig.Process(group.prefix, group.target); err != nil {
			// envconfig can include raw values. Keep secrets out of stderr and logs.
			return fmt.Errorf("load %s configuration: invalid or missing environment variable", strings.ToLower(group.prefix))
		}
	}
	return nil
}

func invalid(message string) error { return fmt.Errorf("invalid configuration: %s", message) }
