package security

import (
	"regexp"
	"strings"
)

// Prometheus label restrictions and security limits
const (
	// MaxLabelLength is the maximum length for a Prometheus label value
	// Prevents DoS via memory exhaustion and cardinality explosion
	MaxLabelLength = 128

	// MaxHeaderLength is the maximum length for response header values
	// Prevents HTTP header injection attacks
	MaxHeaderLength = 256
)

var (
	// prometheusLabelSafeRegex matches characters safe for Prometheus labels
	// Allows: alphanumeric, underscore, hyphen, dot, colon, forward slash
	// These are safe and commonly used in user IDs, tiers, model names
	prometheusLabelSafeRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.:/]+$`)

	// headerSafeRegex matches characters safe for HTTP headers
	// Prevents CRLF injection and header smuggling attacks
	// Allows: alphanumeric, space, hyphen, underscore, dot, colon, forward slash
	headerSafeRegex = regexp.MustCompile(`^[a-zA-Z0-9 _\-\.:/]+$`)

	// Dangerous patterns that could indicate attack attempts
	crlfPattern        = regexp.MustCompile(`[\r\n]`)
	controlCharPattern = regexp.MustCompile(`[\x00-\x1F\x7F]`)
)

// SanitizePrometheusLabel sanitizes a string for use as a Prometheus label value
// Returns sanitized value and whether it was modified
func SanitizePrometheusLabel(value string) (string, bool) {
	if value == "" {
		return "unknown", true
	}

	original := value
	modified := false

	// Trim whitespace
	value = strings.TrimSpace(value)

	// Enforce maximum length to prevent cardinality explosion
	if len(value) > MaxLabelLength {
		value = value[:MaxLabelLength]
		modified = true
	}

	// Replace unsafe characters with underscores
	if !prometheusLabelSafeRegex.MatchString(value) {
		// Replace any non-safe characters with underscore
		value = replaceUnsafeChars(value, prometheusLabelSafeRegex)
		modified = true
	}

	// If completely sanitized to empty, use unknown
	if value == "" || strings.Trim(value, "_") == "" {
		value = "unknown"
		modified = true
	}

	return value, modified || (value != original)
}

// SanitizeHTTPHeader sanitizes a string for use as an HTTP header value
// Returns sanitized value and whether it was modified
func SanitizeHTTPHeader(value string) (string, bool) {
	if value == "" {
		return "", false
	}

	original := value

	// Trim whitespace
	value = strings.TrimSpace(value)

	// Check for CRLF injection attempts (critical security issue)
	if crlfPattern.MatchString(value) {
		// Remove CRLF characters to prevent header injection
		value = crlfPattern.ReplaceAllString(value, "")
	}

	// Remove control characters that could break HTTP parsing
	if controlCharPattern.MatchString(value) {
		value = controlCharPattern.ReplaceAllString(value, "")
	}

	// Enforce maximum length to prevent DoS
	if len(value) > MaxHeaderLength {
		value = value[:MaxHeaderLength]
	}

	// Ensure header value is safe (alphanumeric + common separators)
	if !headerSafeRegex.MatchString(value) {
		value = replaceUnsafeChars(value, headerSafeRegex)
	}

	return value, value != original
}

// replaceUnsafeChars replaces characters that don't match the regex with underscores
func replaceUnsafeChars(s string, safeRegex *regexp.Regexp) string {
	var result strings.Builder
	result.Grow(len(s))

	for _, r := range s {
		char := string(r)
		if safeRegex.MatchString(char) {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}

	return result.String()
}

// ValidateTrustedHeader checks if a header should be trusted
// Returns true if the header appears to come from a trusted source
// This is a defense-in-depth check, but network-level security is primary
func ValidateTrustedHeader(headerName, headerValue string) (bool, string) {
	// Check for suspicious patterns that might indicate spoofing

	// Empty values are suspicious (should use fallback, not empty)
	if headerValue == "" {
		return false, "empty header value"
	}

	// Extremely long values could indicate attack
	if len(headerValue) > MaxLabelLength {
		return false, "header value exceeds maximum length"
	}

	// CRLF injection attempt
	if crlfPattern.MatchString(headerValue) {
		return false, "header contains CRLF characters (injection attempt)"
	}

	// Control characters
	if controlCharPattern.MatchString(headerValue) {
		return false, "header contains control characters"
	}

	// SQL injection patterns (defense in depth, shouldn't be stored in DB)
	sqlPatterns := []string{
		"--", "/*", "*/", ";--", "';", "\"", "DROP", "SELECT", "INSERT", "UPDATE", "DELETE",
	}
	upperValue := strings.ToUpper(headerValue)
	for _, pattern := range sqlPatterns {
		if strings.Contains(upperValue, pattern) {
			return false, "header contains suspicious SQL-like patterns"
		}
	}

	// Shell injection patterns (defense in depth)
	shellPatterns := []string{
		"&&", "||", "|", "`", "$", "$(", "${",
	}
	for _, pattern := range shellPatterns {
		if strings.Contains(headerValue, pattern) {
			return false, "header contains suspicious shell-like patterns"
		}
	}

	return true, ""
}

// SanitizeMaasUser sanitizes and validates a MaaS user identifier
func SanitizeMaasUser(user string) (string, bool) {
	// Sanitize for Prometheus label safety
	sanitized, modified := SanitizePrometheusLabel(user)

	// Additional validation: user IDs should be reasonable
	// Typically: email addresses, UUIDs, or alphanumeric IDs
	if len(sanitized) < 2 {
		return "unknown", true
	}

	return sanitized, modified
}

// SanitizeMaasTier sanitizes and validates a MaaS tier identifier
func SanitizeMaasTier(tier string) (string, bool) {
	// Sanitize for Prometheus label safety
	sanitized, modified := SanitizePrometheusLabel(tier)

	// Validate against known tiers (defense in depth)
	// This is informational only - MaaS gateway is authoritative
	knownTiers := map[string]bool{
		"free":       true,
		"basic":      true,
		"premium":    true,
		"enterprise": true,
		"trial":      true,
		"unknown":    true,
	}

	lowerTier := strings.ToLower(sanitized)
	if !knownTiers[lowerTier] {
		// Log warning but allow (MaaS may have custom tiers)
		return sanitized, true
	}

	return sanitized, modified
}
