package config

import "net"

func validateHTTP(c HTTPConfig, environment string) error {
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return invalid("http address is invalid")
	}
	if c.TLSMode != "disabled" && c.TLSMode != "terminated" {
		return invalid("http tls mode is invalid")
	}
	if environment == productionEnvironment && c.TLSMode == "disabled" {
		return invalid("http tls must be enabled in production")
	}
	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return invalid("http timeouts must be positive")
	}
	if c.MaxHeaderBytes <= 0 || c.MaxBodyBytes <= 0 {
		return invalid("http limits must be positive")
	}
	return nil
}
