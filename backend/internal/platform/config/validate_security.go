package config

import (
	"net"
	"net/url"
)

func validateAuth(c AuthConfig, environment, tlsMode string) error {
	if c.JWTIssuer == "" || c.JWTAudience == "" || c.JWTPrivateKey == "" || c.RefreshTokenPepper == "" {
		return invalid("auth required secret or identifier is missing")
	}
	if c.JWTAlgorithm != approvedJWTAlgorithm {
		return invalid("auth jwt algorithm must be EdDSA")
	}
	if c.CookieSameSite != "strict" {
		return invalid("auth cookie same-site policy must be strict")
	}
	if environment == productionEnvironment && (!c.CookieSecure || tlsMode == "disabled") {
		return invalid("auth cookie or tls settings are unsafe for production")
	}
	if c.AccessTokenTTL <= 0 || c.RefreshAbsoluteTTL <= 0 || c.RefreshIdleTTL <= 0 || c.ClockSkew < 0 || c.RefreshIdleTTL > c.RefreshAbsoluteTTL {
		return invalid("auth ttl settings are invalid")
	}
	return nil
}

func validateProxyCORS(c ProxyCORSConfig) error {
	if c.TrustForwardedHeaders && len(c.TrustedProxyCIDRs) == 0 {
		return invalid("trusted proxy allowlist is required when forwarded headers are trusted")
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return invalid("trusted proxy cidr is invalid")
		}
	}
	for _, origin := range c.AllowedOrigins {
		if origin == "*" && c.AllowCredentials {
			return invalid("cors wildcard origin cannot be used with credentials")
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			return invalid("cors allowed origin is invalid")
		}
	}
	return nil
}
