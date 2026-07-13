package shost

import (
	"os"
	"strings"
)

// Environment names the runtime environment of the host, mirroring
// IHostEnvironment. Comparisons are case-insensitive; custom values are
// allowed.
type Environment string

const (
	Development Environment = "Development"
	Staging     Environment = "Staging"
	Production  Environment = "Production"
)

// DefaultEnvironmentVar is the environment variable consulted by
// EnvironmentFromEnv when no name is given.
const DefaultEnvironmentVar = "APP_ENVIRONMENT"

// EnvironmentFromEnv reads the environment from the named OS variable
// (DefaultEnvironmentVar when varName is empty). An unset or empty
// variable means Production — the safe default.
func EnvironmentFromEnv(varName string) Environment {
	if varName == "" {
		varName = DefaultEnvironmentVar
	}
	if v := strings.TrimSpace(os.Getenv(varName)); v != "" {
		return Environment(v)
	}
	return Production
}

// Is reports whether e equals other, ignoring case.
func (e Environment) Is(other Environment) bool {
	return strings.EqualFold(string(e), string(other))
}

func (e Environment) IsDevelopment() bool { return e.Is(Development) }
func (e Environment) IsStaging() bool     { return e.Is(Staging) }
func (e Environment) IsProduction() bool  { return e.Is(Production) }

func (e Environment) String() string { return string(e) }
