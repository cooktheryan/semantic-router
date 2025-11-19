package config

import (
	"testing"
)

// TestMaasBackwardCompatibility verifies that missing maas_integration config
// results in safe defaults that preserve existing behavior
func TestMaasBackwardCompatibility(t *testing.T) {
	// Create a config with no maas_integration section (zero value)
	cfg := &RouterConfig{}

	// Test that MaaS is disabled by default
	if cfg.IsMaasIntegrationEnabled() {
		t.Error("Expected MaaS integration to be disabled by default")
	}

	// Test that cost calculation is internal when MaaS is disabled
	if !cfg.ShouldCalculateCostsInternally() {
		t.Error("Expected internal cost calculation when MaaS is disabled")
	}

	// Test that MaaS metrics are not exported when disabled
	if cfg.ShouldExportTokenMetrics() {
		t.Error("Expected token metrics NOT to be exported when MaaS is disabled")
	}

	if cfg.ShouldExportCacheMetrics() {
		t.Error("Expected cache metrics NOT to be exported when MaaS is disabled")
	}

	if cfg.ShouldExportRoutingMetrics() {
		t.Error("Expected routing metrics NOT to be exported when MaaS is disabled")
	}

	if cfg.ShouldExportSecurityMetrics() {
		t.Error("Expected security metrics NOT to be exported when MaaS is disabled")
	}

	// Test that MaaS headers are not exported when disabled
	if cfg.ShouldExportRoutingHeaders() {
		t.Error("Expected routing headers NOT to be exported when MaaS is disabled")
	}

	if cfg.ShouldExportCacheHeaders() {
		t.Error("Expected cache headers NOT to be exported when MaaS is disabled")
	}

	if cfg.ShouldExportSecurityHeaders() {
		t.Error("Expected security headers NOT to be exported when MaaS is disabled")
	}

	// Test that header helper methods return safe defaults
	if cfg.GetMaasUserHeader() != "x-auth-request-user" {
		t.Errorf("Expected default user header, got %s", cfg.GetMaasUserHeader())
	}

	if cfg.GetMaasTierHeader() != "x-auth-request-tier" {
		t.Errorf("Expected default tier header, got %s", cfg.GetMaasTierHeader())
	}

	if cfg.GetMaasFallbackUser() != "unknown" {
		t.Errorf("Expected default fallback user, got %s", cfg.GetMaasFallbackUser())
	}

	if cfg.GetMaasFallbackTier() != "free" {
		t.Errorf("Expected default fallback tier, got %s", cfg.GetMaasFallbackTier())
	}

	if cfg.GetMaasHeaderPrefix() != "x-vsr-" {
		t.Errorf("Expected default header prefix, got %s", cfg.GetMaasHeaderPrefix())
	}
}

// TestMaasEnabledBehavior verifies that MaaS features work when enabled
func TestMaasEnabledBehavior(t *testing.T) {
	cfg := &RouterConfig{
		MaasIntegration: MaasIntegrationConfig{
			Enabled: true,
			Metrics: MaasMetricsConfig{
				ExportTokenMetrics:      true,
				ExportCacheMetrics:      true,
				ExportRoutingMetrics:    true,
				ExportSecurityMetrics:   true,
				InternalCostCalculation: false,
			},
			Headers: MaasHeadersConfig{
				ExportRouting:  true,
				ExportCache:    true,
				ExportSecurity: true,
				Prefix:         "x-custom-",
			},
		},
	}

	// Test that MaaS is enabled
	if !cfg.IsMaasIntegrationEnabled() {
		t.Error("Expected MaaS integration to be enabled")
	}

	// Test that cost calculation is deferred to MaaS
	if cfg.ShouldCalculateCostsInternally() {
		t.Error("Expected cost calculation to be deferred to MaaS")
	}

	// Test that MaaS metrics are exported
	if !cfg.ShouldExportTokenMetrics() {
		t.Error("Expected token metrics to be exported when MaaS is enabled")
	}

	if !cfg.ShouldExportCacheMetrics() {
		t.Error("Expected cache metrics to be exported when MaaS is enabled")
	}

	if !cfg.ShouldExportRoutingMetrics() {
		t.Error("Expected routing metrics to be exported when MaaS is enabled")
	}

	if !cfg.ShouldExportSecurityMetrics() {
		t.Error("Expected security metrics to be exported when MaaS is enabled")
	}

	// Test that MaaS headers are exported
	if !cfg.ShouldExportRoutingHeaders() {
		t.Error("Expected routing headers to be exported when MaaS is enabled")
	}

	if !cfg.ShouldExportCacheHeaders() {
		t.Error("Expected cache headers to be exported when MaaS is enabled")
	}

	if !cfg.ShouldExportSecurityHeaders() {
		t.Error("Expected security headers to be exported when MaaS is enabled")
	}

	// Test custom header prefix
	if cfg.GetMaasHeaderPrefix() != "x-custom-" {
		t.Errorf("Expected custom header prefix, got %s", cfg.GetMaasHeaderPrefix())
	}
}

// TestMaasHybridMode verifies hybrid mode (MaaS + internal cost calculation)
func TestMaasHybridMode(t *testing.T) {
	cfg := &RouterConfig{
		MaasIntegration: MaasIntegrationConfig{
			Enabled: true,
			Metrics: MaasMetricsConfig{
				ExportTokenMetrics:      true,
				InternalCostCalculation: true, // Keep internal calculation for validation
			},
			Headers: MaasHeadersConfig{
				ExportRouting: true,
			},
		},
	}

	// Test that MaaS is enabled
	if !cfg.IsMaasIntegrationEnabled() {
		t.Error("Expected MaaS integration to be enabled in hybrid mode")
	}

	// Test that cost calculation is ALSO done internally (hybrid mode)
	if !cfg.ShouldCalculateCostsInternally() {
		t.Error("Expected internal cost calculation in hybrid mode")
	}

	// Test that MaaS metrics are still exported
	if !cfg.ShouldExportTokenMetrics() {
		t.Error("Expected token metrics to be exported in hybrid mode")
	}
}
