package config

// DefaultIgnorePatterns returns patterns that are always excluded from all layers.
var DefaultIgnorePatterns = []string{
	".env.local",
	".env.*.local",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"token.json",
	"credentials",
	".git/credentials",
	".aws/credentials",
	".ssh/*",
	".git/**",
	".DS_Store",
	"Thumbs.db",
	"*.swp",
	"*.swo",
	"*~",
}
