package config

import (
	"strings"
	"testing"
)

// TestCheckDeprecatedEnvVars_EmptyRegistry pins the real state: with the
// committed (empty) deprecatedEnvVars, boot emits no deprecation warning
// regardless of the environment.
func TestCheckDeprecatedEnvVars_EmptyRegistry(t *testing.T) {
	var warnings []string
	checkDeprecatedEnvVars(
		func(string) (string, bool) { return "anything", true },
		func(msg string) { warnings = append(warnings, msg) },
	)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings from an empty registry, got %q", warnings)
	}
}

// TestCheckDeprecatedEnvVars_Behavior drives the function with a fixture
// registry: one warning naming the replacement for each *set* variable, and
// nothing for the unset ones.
func TestCheckDeprecatedEnvVars_Behavior(t *testing.T) {
	restore := deprecatedEnvVars
	deprecatedEnvVars = map[string]deprecatedEnvVar{
		"OLD_A": {Replacement: "NEW_A", Since: "v0.7.0"},
		"OLD_B": {Replacement: "NEW_B", Since: "v0.8.0"},
		"OLD_C": {Replacement: "NEW_C", Since: "v0.8.0"},
	}
	t.Cleanup(func() { deprecatedEnvVars = restore })

	set := map[string]string{"OLD_A": "1", "OLD_C": "/tmp/x"}
	var warnings []string
	checkDeprecatedEnvVars(
		func(k string) (string, bool) { v, ok := set[k]; return v, ok },
		func(msg string) { warnings = append(warnings, msg) },
	)

	got := strings.Join(warnings, "\n")
	for _, want := range []string{
		"WARN: OLD_A is deprecated (since v0.7.0); use NEW_A instead",
		"WARN: OLD_C is deprecated (since v0.8.0); use NEW_C instead",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing warning %q; got:\n%s", want, got)
		}
	}
	if len(warnings) != 2 {
		t.Fatalf("expected exactly 2 warnings (OLD_B is not set), got %d:\n%s", len(warnings), got)
	}
}

// TestValidateOrPanic_RunsDeprecatedEnvVarCheck covers the wiring: a clean
// config validates without panicking, and with a fixture deprecated variable
// set, the boot path surfaces the WARN.
func TestValidateOrPanic_RunsDeprecatedEnvVarCheck(t *testing.T) {
	restore := deprecatedEnvVars
	deprecatedEnvVars = map[string]deprecatedEnvVar{"OLD_BOOT": {Replacement: "NEW_BOOT", Since: "v0.7.0"}}
	t.Cleanup(func() { deprecatedEnvVars = restore })
	t.Setenv("OLD_BOOT", "1")

	// Should not panic on a valid configuration.
	validConfig().ValidateOrPanic()
}

// TestCheckDeprecatedEnvVars_UnsetIsSilent is the narrow guarantee the policy
// makes: a deprecated variable that is not set produces nothing at all.
func TestCheckDeprecatedEnvVars_UnsetIsSilent(t *testing.T) {
	restore := deprecatedEnvVars
	deprecatedEnvVars = map[string]deprecatedEnvVar{"OLD_X": {Replacement: "NEW_X", Since: "v0.7.0"}}
	t.Cleanup(func() { deprecatedEnvVars = restore })

	var warnings []string
	checkDeprecatedEnvVars(
		func(string) (string, bool) { return "", false },
		func(msg string) { warnings = append(warnings, msg) },
	)
	if len(warnings) != 0 {
		t.Fatalf("expected silence for an unset variable, got %q", warnings)
	}
}
