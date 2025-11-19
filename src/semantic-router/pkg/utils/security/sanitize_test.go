package security

import (
	"testing"
)

func TestSanitizePrometheusLabel(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedOutput string
		expectedModified bool
	}{
		{
			name:             "empty string",
			input:            "",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
		{
			name:             "safe alphanumeric",
			input:            "user123",
			expectedOutput:   "user123",
			expectedModified: false,
		},
		{
			name:             "safe with hyphen and underscore",
			input:            "user-tier_premium",
			expectedOutput:   "user-tier_premium",
			expectedModified: false,
		},
		{
			name:             "safe with dots and colons",
			input:            "model:qwen3.5",
			expectedOutput:   "model:qwen3.5",
			expectedModified: false,
		},
		{
			name:             "safe with forward slash",
			input:            "tier/premium",
			expectedOutput:   "tier/premium",
			expectedModified: false,
		},
		{
			name:             "unsafe characters - spaces",
			input:            "user name with spaces",
			expectedOutput:   "user_name_with_spaces",
			expectedModified: true,
		},
		{
			name:             "unsafe characters - special chars",
			input:            "user@example.com",
			expectedOutput:   "user_example.com",
			expectedModified: true,
		},
		{
			name:             "CRLF injection attempt",
			input:            "user\r\nmalicious",
			expectedOutput:   "user__malicious",
			expectedModified: true,
		},
		{
			name:             "SQL injection attempt",
			input:            "user'; DROP TABLE users--",
			expectedOutput:   "user___DROP_TABLE_users--", // Hyphens are allowed in Prometheus labels
			expectedModified: true,
		},
		{
			name:             "shell injection attempt",
			input:            "user && rm -rf /",
			expectedOutput:   "user____rm_-rf_/", // Hyphens and forward slashes are allowed
			expectedModified: true,
		},
		{
			name:             "exceeds max length",
			input:            "verylongusernamethatexceedsthemaximumlengthof128charactersandshouldbetrimmedtopreventprometheuscardinality" + "explosion" + "problemsthatcouldleadtodenialofservice",
			expectedOutput:   "verylongusernamethatexceedsthemaximumlengthof128charactersandshouldbetrimmedtopreventprometheuscardinalityexplosionproblemsthatc", // Trimmed to 128 chars
			expectedModified: true,
		},
		{
			name:             "only special characters",
			input:            "!@#$%^&*()",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
		{
			name:             "only whitespace",
			input:            "   ",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
		{
			name:             "whitespace trimming",
			input:            "  user123  ",
			expectedOutput:   "user123",
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, modified := SanitizePrometheusLabel(tt.input)
			if output != tt.expectedOutput {
				t.Errorf("SanitizePrometheusLabel(%q) = %q, want %q", tt.input, output, tt.expectedOutput)
			}
			if modified != tt.expectedModified {
				t.Errorf("SanitizePrometheusLabel(%q) modified = %v, want %v", tt.input, modified, tt.expectedModified)
			}
		})
	}
}

func TestSanitizeHTTPHeader(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedOutput   string
		expectedModified bool
	}{
		{
			name:             "empty string",
			input:            "",
			expectedOutput:   "",
			expectedModified: false,
		},
		{
			name:             "safe alphanumeric",
			input:            "qwen3",
			expectedOutput:   "qwen3",
			expectedModified: false,
		},
		{
			name:             "safe with spaces",
			input:            "model name",
			expectedOutput:   "model name",
			expectedModified: false,
		},
		{
			name:             "CRLF injection attempt",
			input:            "value\r\nMalicious-Header: injected",
			expectedOutput:   "valueMalicious-Header: injected",
			expectedModified: true,
		},
		{
			name:             "control characters",
			input:            "value\x00\x01\x02",
			expectedOutput:   "value",
			expectedModified: true,
		},
		{
			name:             "null byte injection",
			input:            "value\x00malicious",
			expectedOutput:   "valuemalicious",
			expectedModified: true,
		},
		{
			name:             "exceeds max header length",
			input:            "verylongheadervaluethatexceedsthemaximumlengthof256charactersandshouldbetrimmedtopreventheaderinjectionattacks" + "anddenialofserviceissuesthatcouldoccurifheadersaretoolongandcauseproblemswithhttpparsingormemoryexhaustion" + "soweenforceareasonablemaximumlengthandmorecharacterstoexceed256limit",
			expectedOutput:   "verylongheadervaluethatexceedsthemaximumlengthof256charactersandshouldbetrimmedtopreventheaderinjectionattacksanddenialofserviceissuesthatcouldoccurifheadersaretoolongandcauseproblemswithhttpparsingormemoryexhaustionsoweenforceareasonablemaximumlengthandmo", // Trimmed to 256 chars
			expectedModified: true,
		},
		{
			name:             "unsafe characters",
			input:            "value<script>alert('xss')</script>",
			expectedOutput:   "value_script_alert__xss___/script_", // Forward slash not allowed in HTTP headers
			expectedModified: true,
		},
		{
			name:             "whitespace trimming",
			input:            "  value  ",
			expectedOutput:   "value",
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, modified := SanitizeHTTPHeader(tt.input)
			if output != tt.expectedOutput {
				t.Errorf("SanitizeHTTPHeader(%q) = %q, want %q", tt.input, output, tt.expectedOutput)
			}
			if modified != tt.expectedModified {
				t.Errorf("SanitizeHTTPHeader(%q) modified = %v, want %v", tt.input, modified, tt.expectedModified)
			}
		})
	}
}

func TestValidateTrustedHeader(t *testing.T) {
	tests := []struct {
		name          string
		headerName    string
		headerValue   string
		expectedValid bool
		expectedReason string
	}{
		{
			name:          "valid user header",
			headerName:    "x-auth-request-user",
			headerValue:   "alice",
			expectedValid: true,
			expectedReason: "",
		},
		{
			name:          "valid tier header",
			headerName:    "x-auth-request-tier",
			headerValue:   "premium",
			expectedValid: true,
			expectedReason: "",
		},
		{
			name:           "empty header value",
			headerName:     "x-auth-request-user",
			headerValue:    "",
			expectedValid:  false,
			expectedReason: "empty header value",
		},
		{
			name:           "header value exceeds max length",
			headerName:     "x-auth-request-user",
			headerValue:    "verylongusernamethatexceedsthemaximumlengthof128charactersandshouldbetrimmedtopreventprometheuscardinality" + "explosionandmoreandsomemore",
			expectedValid:  false,
			expectedReason: "header value exceeds maximum length",
		},
		{
			name:           "CRLF injection attempt",
			headerName:     "x-auth-request-user",
			headerValue:    "user\r\nmalicious",
			expectedValid:  false,
			expectedReason: "header contains CRLF characters (injection attempt)",
		},
		{
			name:           "control characters",
			headerName:     "x-auth-request-user",
			headerValue:    "user\x00malicious",
			expectedValid:  false,
			expectedReason: "header contains control characters",
		},
		{
			name:           "SQL injection attempt - comment",
			headerName:     "x-auth-request-user",
			headerValue:    "user'; DROP TABLE users--",
			expectedValid:  false,
			expectedReason: "header contains suspicious SQL-like patterns",
		},
		{
			name:           "SQL injection attempt - SELECT",
			headerName:     "x-auth-request-user",
			headerValue:    "user' OR '1'='1'; SELECT * FROM users",
			expectedValid:  false,
			expectedReason: "header contains suspicious SQL-like patterns",
		},
		{
			name:           "shell injection attempt - pipe",
			headerName:     "x-auth-request-user",
			headerValue:    "user | cat /etc/passwd",
			expectedValid:  false,
			expectedReason: "header contains suspicious shell-like patterns",
		},
		{
			name:           "shell injection attempt - command substitution",
			headerName:     "x-auth-request-user",
			headerValue:    "user$(whoami)",
			expectedValid:  false,
			expectedReason: "header contains suspicious shell-like patterns",
		},
		{
			name:           "shell injection attempt - backticks",
			headerName:     "x-auth-request-user",
			headerValue:    "user`whoami`",
			expectedValid:  false,
			expectedReason: "header contains suspicious shell-like patterns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, reason := ValidateTrustedHeader(tt.headerName, tt.headerValue)
			if valid != tt.expectedValid {
				t.Errorf("ValidateTrustedHeader(%q, %q) valid = %v, want %v", tt.headerName, tt.headerValue, valid, tt.expectedValid)
			}
			if reason != tt.expectedReason {
				t.Errorf("ValidateTrustedHeader(%q, %q) reason = %q, want %q", tt.headerName, tt.headerValue, reason, tt.expectedReason)
			}
		})
	}
}

func TestSanitizeMaasUser(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedOutput   string
		expectedModified bool
	}{
		{
			name:             "valid user",
			input:            "alice",
			expectedOutput:   "alice",
			expectedModified: false,
		},
		{
			name:             "valid email",
			input:            "alice@example.com",
			expectedOutput:   "alice_example.com",
			expectedModified: true,
		},
		{
			name:             "valid UUID",
			input:            "123e4567-e89b-12d3-a456-426614174000",
			expectedOutput:   "123e4567-e89b-12d3-a456-426614174000",
			expectedModified: false,
		},
		{
			name:             "too short (single char)",
			input:            "a",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
		{
			name:             "empty string",
			input:            "",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, modified := SanitizeMaasUser(tt.input)
			if output != tt.expectedOutput {
				t.Errorf("SanitizeMaasUser(%q) = %q, want %q", tt.input, output, tt.expectedOutput)
			}
			if modified != tt.expectedModified {
				t.Errorf("SanitizeMaasUser(%q) modified = %v, want %v", tt.input, modified, tt.expectedModified)
			}
		})
	}
}

func TestSanitizeMaasTier(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedOutput   string
		expectedModified bool
	}{
		{
			name:             "known tier - free",
			input:            "free",
			expectedOutput:   "free",
			expectedModified: false,
		},
		{
			name:             "known tier - basic",
			input:            "basic",
			expectedOutput:   "basic",
			expectedModified: false,
		},
		{
			name:             "known tier - premium",
			input:            "premium",
			expectedOutput:   "premium",
			expectedModified: false,
		},
		{
			name:             "known tier - enterprise",
			input:            "enterprise",
			expectedOutput:   "enterprise",
			expectedModified: false,
		},
		{
			name:             "known tier - trial",
			input:            "trial",
			expectedOutput:   "trial",
			expectedModified: false,
		},
		{
			name:             "case insensitive - PREMIUM",
			input:            "PREMIUM",
			expectedOutput:   "PREMIUM",
			expectedModified: false,
		},
		{
			name:             "unknown tier",
			input:            "custom-tier",
			expectedOutput:   "custom-tier",
			expectedModified: true,
		},
		{
			name:             "empty string",
			input:            "",
			expectedOutput:   "unknown",
			expectedModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, modified := SanitizeMaasTier(tt.input)
			if output != tt.expectedOutput {
				t.Errorf("SanitizeMaasTier(%q) = %q, want %q", tt.input, output, tt.expectedOutput)
			}
			if modified != tt.expectedModified {
				t.Errorf("SanitizeMaasTier(%q) modified = %v, want %v", tt.input, modified, tt.expectedModified)
			}
		})
	}
}
