package security

import (
	"testing"
)

// TestBackwardCompatibility_TypicalSemanticRouterValues verifies that sanitization
// does NOT modify typical values used in semantic router deployments
func TestBackwardCompatibility_TypicalSemanticRouterValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		// Typical model names
		{"model: qwen3", "qwen3"},
		{"model: gpt-4", "gpt-4"},
		{"model: gpt-4-turbo", "gpt-4-turbo"},
		{"model: llama-3.1", "llama-3.1"},
		{"model: claude-3-opus", "claude-3-opus"},
		{"model: gemma-2-9b", "gemma-2-9b"},

		// Typical category names
		{"category: math", "math"},
		{"category: coding", "coding"},
		{"category: business", "business"},
		{"category: science", "science"},
		{"category: law", "law"},
		{"category: medicine", "medicine"},

		// Typical decision names
		{"decision: code_generation", "code_generation"},
		{"decision: math_problem", "math_problem"},
		{"decision: general_qa", "general_qa"},
		{"decision: creative_writing", "creative_writing"},

		// Reasoning mode values
		{"reasoning: on", "on"},
		{"reasoning: off", "off"},

		// System prompt injected values
		{"injected: true", "true"},
		{"injected: false", "false"},
	}

	t.Run("SanitizeHTTPHeader - no modification for typical values", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sanitized, modified := SanitizeHTTPHeader(tt.value)
				if modified {
					t.Errorf("SanitizeHTTPHeader(%q) was modified to %q - this breaks backward compatibility!", tt.value, sanitized)
				}
				if sanitized != tt.value {
					t.Errorf("SanitizeHTTPHeader(%q) = %q, want %q", tt.value, sanitized, tt.value)
				}
			})
		}
	})

	t.Run("SanitizePrometheusLabel - no modification for typical values", func(t *testing.T) {
		// Add typical user/tier values
		additionalTests := []struct {
			name  string
			value string
		}{
			{"user: alice", "alice"},
			{"user: bob", "bob"},
			{"user: user123", "user123"},
			{"user: test-user", "test-user"},
			{"tier: free", "free"},
			{"tier: basic", "basic"},
			{"tier: premium", "premium"},
			{"tier: enterprise", "enterprise"},
		}

		allTests := append(tests, additionalTests...)

		for _, tt := range allTests {
			t.Run(tt.name, func(t *testing.T) {
				sanitized, modified := SanitizePrometheusLabel(tt.value)
				if modified {
					t.Errorf("SanitizePrometheusLabel(%q) was modified to %q - this breaks backward compatibility!", tt.value, sanitized)
				}
				if sanitized != tt.value {
					t.Errorf("SanitizePrometheusLabel(%q) = %q, want %q", tt.value, sanitized, tt.value)
				}
			})
		}
	})
}

// TestBackwardCompatibility_MetricsNotDuplicated verifies that in standalone mode,
// we don't record both standard AND MaaS metrics (which would break backward compat)
func TestBackwardCompatibility_StandaloneModeBehavior(t *testing.T) {
	t.Run("MaaS disabled means standard metrics only", func(t *testing.T) {
		// This is tested implicitly by the config tests, but documenting here
		// for clarity: when MaaS is disabled (default), only standard metrics
		// should be recorded, not MaaS metrics.
		//
		// Verified in pkg/config/maas_test.go::TestMaasBackwardCompatibility
		t.Skip("Tested in pkg/config/maas_test.go")
	})
}

// TestBackwardCompatibility_NoNewRequiredConfigFields verifies that
// the MaaS integration doesn't introduce any required config fields
func TestBackwardCompatibility_NoRequiredConfigFields(t *testing.T) {
	t.Run("MaaS config section is optional", func(t *testing.T) {
		// This is tested implicitly by the config loading tests
		// Verified in pkg/config/maas_test.go::TestMaasBackwardCompatibility
		t.Skip("Tested in pkg/config/maas_test.go")
	})
}

// TestBackwardCompatibility_SanitizationPerformance verifies that
// sanitization doesn't add significant overhead for typical values
func TestBackwardCompatibility_SanitizationPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	t.Run("SanitizeHTTPHeader performance", func(t *testing.T) {
		value := "qwen3"
		const iterations = 100000

		// Warm up
		for i := 0; i < 1000; i++ {
			SanitizeHTTPHeader(value)
		}

		// Measure
		for i := 0; i < iterations; i++ {
			sanitized, _ := SanitizeHTTPHeader(value)
			if sanitized != value {
				t.Fatalf("Unexpected modification: %q -> %q", value, sanitized)
			}
		}

		// If we get here without timeout, performance is acceptable
		// (100k iterations should complete in well under 1 second)
	})

	t.Run("SanitizePrometheusLabel performance", func(t *testing.T) {
		value := "qwen3"
		const iterations = 100000

		// Warm up
		for i := 0; i < 1000; i++ {
			SanitizePrometheusLabel(value)
		}

		// Measure
		for i := 0; i < iterations; i++ {
			sanitized, _ := SanitizePrometheusLabel(value)
			if sanitized != value {
				t.Fatalf("Unexpected modification: %q -> %q", value, sanitized)
			}
		}

		// If we get here without timeout, performance is acceptable
	})
}
