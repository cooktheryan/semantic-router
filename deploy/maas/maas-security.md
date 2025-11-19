# MaaS Integration Security Considerations

This document details the security architecture, threat model, and mitigations for the MaaS-billing integration in semantic router.

## Table of Contents

1. [Security Architecture](#security-architecture)
2. [Threat Model](#threat-model)
3. [Security Mitigations](#security-mitigations)
4. [Defense-in-Depth Layers](#defense-in-depth-layers)
5. [Security Best Practices](#security-best-practices)
6. [Incident Response](#incident-response)

---

## Security Architecture

### Trust Boundaries

The MaaS integration establishes clear trust boundaries:

```
┌─────────────────────────────────────────────────────────────┐
│ UNTRUSTED ZONE                                              │
│                                                             │
│  ┌─────────┐                                               │
│  │ Client  │  ← Cannot be trusted                          │
│  └─────────┘                                               │
└─────────────────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────────────┐
│ AUTHENTICATION ZONE (Trust Boundary)                        │
│                                                             │
│  ┌──────────────────────┐                                  │
│  │  MaaS Gateway        │                                  │
│  │  (Kuadrant/Authorino)│  ← Authenticates user           │
│  └──────────────────────┘  ← Sets auth headers             │
│                            ← Enforces rate limits           │
└─────────────────────────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────────────────────────┐
│ TRUSTED ZONE                                                │
│                                                             │
│  ┌──────────────────────┐      ┌──────────────────────┐   │
│  │  Semantic Router     │─────→│  vLLM Backend        │   │
│  │  (ExtProc Filter)    │      │                      │   │
│  └──────────────────────┘      └──────────────────────┘   │
│           │                                                 │
│           ↓                                                 │
│  ┌──────────────────────┐                                  │
│  │  Prometheus          │                                  │
│  │  (MaaS-billing)      │                                  │
│  └──────────────────────┘                                  │
└─────────────────────────────────────────────────────────────┘
```

### Security Principles

1. **Network-Level Security is Primary**: Authentication happens at the MaaS gateway (Kuadrant/Authorino)
2. **Defense in Depth**: Semantic router sanitizes and validates all inputs even when they should be trusted
3. **Fail Secure**: On validation failure, use safe fallback values (e.g., "unknown" user/tier)
4. **Least Privilege**: Only expose necessary information in metrics and headers
5. **Audit Logging**: Log all security-relevant events for investigation

---

## Threat Model

### Threat: Header Spoofing

**Description**: Malicious client attempts to set authentication headers (e.g., `x-auth-request-user`, `x-auth-request-tier`) to impersonate another user or tier.

**Impact**:
- Billing fraud (charge another user's account)
- Access control bypass (use higher tier privileges)
- Usage tracking evasion (hide malicious activity)

**Likelihood**: HIGH (if semantic router is directly accessible)

**Mitigation Strategy**:

1. **PRIMARY DEFENSE (Network-Level)**:
   - Semantic router MUST NOT be directly accessible by clients
   - All traffic MUST flow through MaaS Gateway (Kuadrant/Authorino)
   - Gateway authenticates user and sets authentication headers
   - Network policies prevent direct access to semantic router

2. **SECONDARY DEFENSE (Application-Level)**:
   - Semantic router validates and sanitizes all authentication headers
   - Suspicious headers trigger warnings and fallback to safe defaults
   - Detection patterns: CRLF injection, SQL injection, shell injection, control characters
   - Maximum length enforcement prevents DoS via cardinality explosion
   - All validation failures are logged for security investigation

**Implementation**: `pkg/utils/security/sanitize.go`, `pkg/extproc/processor_req_header.go:96-144`

---

### Threat: Prometheus Label Injection

**Description**: Attacker injects special characters into metric labels to:
- Cause Prometheus cardinality explosion (DoS)
- Inject malicious queries via PromQL
- Corrupt billing data

**Impact**:
- Prometheus memory exhaustion
- Billing inaccuracies
- Monitoring system failure

**Likelihood**: MEDIUM (requires header spoofing or malicious model names)

**Mitigation Strategy**:

1. **Sanitization**: All metric label values are sanitized using `SanitizePrometheusLabel()`:
   - Enforces maximum length (128 characters) to prevent cardinality explosion
   - Removes unsafe characters (only allows: `[a-zA-Z0-9_\-\.:\/]`)
   - Replaces dangerous characters with underscores
   - Empty/invalid values default to "unknown"

2. **Defense in Depth**: Sanitization applied at multiple layers:
   - User/tier: Sanitized in header extraction (`processor_req_header.go`)
   - Model: Sanitized in metric recording (`metrics.go`)
   - Decision: Sanitized in metric recording (even though from config)
   - PII Type: Sanitized in metric recording (even though from classifier)

**Implementation**: `pkg/utils/security/sanitize.go:35-68`, `pkg/observability/metrics/metrics.go:948-1075`

---

### Threat: HTTP Header Injection

**Description**: Attacker injects CRLF characters (`\r\n`) into response headers to:
- Perform HTTP response splitting
- Inject additional headers (e.g., `Set-Cookie`)
- Bypass security controls

**Impact**:
- Session hijacking
- Cache poisoning
- Cross-site scripting (XSS)

**Likelihood**: LOW (response headers derived from internal processing)

**Mitigation Strategy**:

1. **Sanitization**: All response header values are sanitized using `SanitizeHTTPHeader()`:
   - Removes CRLF characters (`\r\n`) to prevent header injection
   - Removes control characters (`\x00-\x1F`, `\x7F`) to prevent protocol confusion
   - Enforces maximum length (256 characters) to prevent DoS
   - Ensures only safe characters: `[a-zA-Z0-9 _\-\.:\/]`

2. **Application Points**:
   - `x-vsr-category`: Sanitized before adding to response
   - `x-vsr-decision`: Sanitized before adding to response
   - `x-vsr-model-selected`: Sanitized before adding to response
   - `x-vsr-reasoning-enabled`: Sanitized before adding to response
   - `x-vsr-cache-hit`: Hardcoded values ("true"/"false"), inherently safe

**Implementation**: `pkg/utils/security/sanitize.go:70-104`, `pkg/extproc/processor_res_header.go:74-187`

---

### Threat: Billing Evasion

**Description**: Attacker manipulates the system to avoid being charged correctly:
- Manipulate token counts to be zero or negative
- Cause JSON parsing errors to hide token usage
- Use malformed responses to evade billing

**Impact**:
- Revenue loss
- Unfair resource usage
- Budget exhaustion for legitimate users

**Likelihood**: LOW (token counts come from vLLM backend, not client)

**Mitigation Strategy**:

1. **Token Count Validation**:
   - Reject negative token counts (set to 0 and log warning)
   - Detect zero token counts for non-empty responses (log warning)
   - Detect absurdly large token counts (>1M tokens, log warning)
   - All anomalies recorded as request errors for investigation

2. **Audit Logging**:
   - All token usage logged with request ID for correlation
   - Warnings logged for suspicious patterns
   - Metrics recorded for billing discrepancies

3. **Trusted Source**:
   - Token counts come from vLLM response (trusted backend)
   - Client cannot manipulate response body
   - Response flows through semantic router before reaching client

**Implementation**: `pkg/extproc/processor_res_body.go:56-84`

---

### Threat: Cache Poisoning

**Description**: Attacker manipulates semantic cache to:
- Inject malicious responses
- Cause incorrect billing (cache hits charged at lower rate)
- Serve incorrect responses to other users

**Impact**:
- Data corruption
- Billing inaccuracies
- User experience degradation

**Likelihood**: LOW (cache keyed by query embedding, difficult to predict)

**Mitigation Strategy**:

1. **Cache Key Security**:
   - Cache key is derived from query embedding (semantic similarity)
   - Embeddings computed by trusted BERT model
   - Difficult for attacker to predict cache keys

2. **Cache Isolation**:
   - Cache entries isolated by model name
   - Query must meet similarity threshold to retrieve cached response
   - Cache entries have TTL to prevent stale data

3. **Cache Hit Validation**:
   - Cache hits logged for audit trail
   - MaaS metrics track cache hit rates per user/tier
   - Unusual cache hit patterns can be detected

**Implementation**: `pkg/cache/*`, `pkg/extproc/req_filter_cache.go`

---

### Threat: Information Disclosure

**Description**: Response headers leak sensitive information:
- Internal model names
- Routing decisions
- System architecture details

**Impact**:
- Reconnaissance for further attacks
- Competitive intelligence leakage
- Privacy violation

**Likelihood**: LOW (response headers contain operational metadata)

**Mitigation Strategy**:

1. **Minimal Information Exposure**:
   - Only expose headers when MaaS integration is enabled
   - Headers configurable via `maas_integration.headers` section
   - Can disable specific header categories (routing, cache, security)

2. **Sanitization**:
   - All header values sanitized to remove potentially sensitive data
   - No internal system paths or configuration details exposed
   - Model names, decisions, categories are operational metadata (not secrets)

3. **Configuration Options**:
   ```yaml
   maas_integration:
     headers:
       export_routing: true   # Can disable if not needed
       export_cache: true     # Can disable if not needed
       export_security: false # Disabled by default
   ```

**Implementation**: `pkg/extproc/processor_res_header.go:54-198`, `config/config.yaml`

---

## Security Mitigations

### Input Validation and Sanitization

All external inputs are validated and sanitized at multiple layers:

#### 1. Authentication Headers (User/Tier)

**Validation**: `ValidateTrustedHeader()`
- Checks for empty values
- Detects CRLF injection attempts (`\r\n`)
- Detects control characters (`\x00-\x1F`, `\x7F`)
- Detects SQL injection patterns (`--`, `/*`, `'`, `DROP`, `SELECT`, etc.)
- Detects shell injection patterns (`&&`, `||`, `` ` ``, `$`, etc.)

**Sanitization**: `SanitizeMaasUser()`, `SanitizeMaasTier()`
- Enforces maximum length (128 characters)
- Allows only safe characters: `[a-zA-Z0-9_\-\.:\/]`
- Trims whitespace
- Defaults to "unknown" on failure

**Fallback on Failure**:
- Untrusted headers → Use fallback values from config
- Log warning for security investigation
- Continue processing with safe defaults

#### 2. Prometheus Labels (Model, Decision, PII Type)

**Sanitization**: `SanitizePrometheusLabel()`
- Enforces maximum length (128 characters)
- Allows only safe characters: `[a-zA-Z0-9_\-\.:\/]`
- Replaces unsafe characters with underscores
- Defaults to "unknown" on empty/invalid input

**Application**:
- Model name: Sanitized in all metric recording functions
- Decision name: Sanitized even though from config (defense in depth)
- PII type: Sanitized even though from classifier (defense in depth)

#### 3. Response Headers (Routing Metadata)

**Sanitization**: `SanitizeHTTPHeader()`
- Removes CRLF characters (`\r\n`)
- Removes control characters (`\x00-\x1F`, `\x7F`)
- Enforces maximum length (256 characters)
- Allows only safe characters: `[a-zA-Z0-9 _\-\.:\/]` (includes space)

**Application**:
- `x-vsr-category`: Sanitized before adding to response
- `x-vsr-decision`: Sanitized before adding to response
- `x-vsr-model-selected`: Sanitized before adding to response
- `x-vsr-reasoning-enabled`: Sanitized before adding to response

#### 4. Token Counts (Billing Data)

**Validation**:
- Reject negative token counts (set to 0, log warning)
- Detect zero token counts for non-empty responses (log warning)
- Detect absurdly large token counts (>1M, log warning)
- Record all anomalies as request errors

---

### Security Logging

All security-relevant events are logged for investigation:

#### Warning-Level Logs

```go
// Untrusted authentication header
logging.Warnf("Untrusted user header detected (%s): %s - using fallback", reason, user)

// Sanitized header value
logging.Warnf("Sanitized MaaS user header from '%s' to '%s' for security", user, sanitizedUser)

// Token count anomalies
logging.Warnf("Security: Negative token counts detected (possible billing evasion): prompt=%d, completion=%d", ...)
logging.Warnf("Security: Zero token counts detected for non-empty response (possible billing evasion): ...", ...)
logging.Warnf("Security: Unusually large token counts detected (possible billing manipulation): ...", ...)
```

#### Error-Level Logs

```go
// JSON parsing failure
logging.Errorf("Error parsing tokens from response: %v", err)
```

#### Event Logs (Structured)

```go
// LLM usage with billing context
logging.LogEvent("llm_usage", map[string]interface{}{
    "request_id": ctx.RequestID,
    "model": ctx.RequestModel,
    "prompt_tokens": promptTokens,
    "completion_tokens": completionTokens,
    "maas_user": ctx.MaasUser,
    "maas_tier": ctx.MaasTier,
    "billing_mode": "maas",
})
```

All logs include:
- Request ID for correlation
- User/tier for accountability
- Detailed context for investigation
- Timestamp (implicit in logging framework)

---

## Defense-in-Depth Layers

The MaaS integration implements multiple layers of security controls:

### Layer 1: Network-Level (Primary Defense)

**Control**: MaaS Gateway (Kuadrant/Authorino)
- Authenticates all requests
- Sets authentication headers after validation
- Enforces rate limits via Limitador
- Blocks direct access to semantic router

**Deployment Requirement**:
```yaml
# Network policy: Block direct access to semantic router
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: semantic-router-ingress
spec:
  podSelector:
    matchLabels:
      app: semantic-router
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: maas-gateway  # Only allow traffic from gateway
```

---

### Layer 2: Application-Level (Secondary Defense)

**Control**: Header Validation and Sanitization
- Validates all authentication headers
- Detects injection attempts (CRLF, SQL, shell)
- Sanitizes values for safe use in metrics/headers
- Logs all suspicious activity

**Rationale**: Defense against:
- Misconfigured network policies
- Compromised gateway
- Internal threats

---

### Layer 3: Data-Level (Tertiary Defense)

**Control**: Metric and Cost Validation
- Validates token counts (non-negative, reasonable)
- Sanitizes all Prometheus label values
- Detects billing anomalies
- Logs all validation failures

**Rationale**: Defense against:
- Malformed responses from vLLM
- Software bugs
- Data corruption

---

## Security Best Practices

### Deployment

1. **Network Isolation**:
   - ✅ Deploy semantic router in private subnet
   - ✅ Block direct access from internet
   - ✅ Only allow traffic from MaaS gateway
   - ✅ Use network policies to enforce isolation

2. **Authentication**:
   - ✅ Use Kuadrant/Authorino for authentication
   - ✅ Configure strong authentication method (OAuth, OIDC, mTLS)
   - ✅ Validate JWT signatures
   - ✅ Enforce short token TTLs

3. **TLS**:
   - ✅ Use TLS for all communication
   - ✅ Enforce TLS 1.2+ (disable older versions)
   - ✅ Use strong cipher suites
   - ✅ Verify certificate chains

4. **Configuration**:
   - ✅ Use strong fallback values (e.g., "unknown" user/tier)
   - ✅ Configure appropriate header names
   - ✅ Enable security logging
   - ✅ Review and test configuration before deployment

### Monitoring

1. **Security Metrics**:
   ```promql
   # Track validation failures
   rate(semantic_router_request_errors_total{error_type=~"invalid_token_count|zero_token_count|excessive_token_count"}[5m])

   # Track unusual billing patterns
   sum by (user, tier) (semantic_router_tokens_total) > 1000000  # Users with >1M tokens

   # Track cache hit rate anomalies
   rate(semantic_router_cache_hits_total[5m]) /
   (rate(semantic_router_cache_hits_total[5m]) + rate(semantic_router_cache_misses_total[5m])) > 0.95  # >95% cache hit rate
   ```

2. **Log Monitoring**:
   - Alert on "Untrusted header detected" warnings
   - Alert on "Sanitized header" warnings
   - Alert on "Security: Negative token counts" warnings
   - Alert on "Security: Zero token counts" warnings
   - Alert on "Security: Unusually large token counts" warnings

3. **Audit**:
   - Regularly review security logs
   - Investigate all validation failures
   - Correlate logs with billing data
   - Monitor for trends and patterns

### Configuration Review

Regularly audit configuration for security issues:

```yaml
# config.yaml
maas_integration:
  enabled: true

  authentication:
    # SECURITY: Ensure header names match gateway configuration
    user_header: "x-auth-request-user"  # ✅ Correct
    tier_header: "x-auth-request-tier"  # ✅ Correct

    # SECURITY: Use safe fallback values
    fallback_user: "unknown"  # ✅ Safe default
    fallback_tier: "free"     # ✅ Safe default (lowest tier)

  headers:
    # SECURITY: Only export headers needed for billing
    export_routing: true   # ✅ Needed for model-based pricing
    export_cache: true     # ✅ Needed for cache-tier pricing
    export_security: false # ✅ Not needed for billing (minimize exposure)
```

---

## Incident Response

### Detection

Security incidents may be detected via:

1. **Security Logs**:
   - "Untrusted header detected" warnings
   - "Sanitized header" warnings
   - Token count validation failures

2. **Metrics Anomalies**:
   - Unusual token usage patterns
   - Abnormal cache hit rates
   - Excessive request errors

3. **Billing Discrepancies**:
   - Revenue lower than expected
   - User complaints about incorrect charges
   - Unusual usage patterns

### Response Procedure

1. **Immediate Actions**:
   - Review security logs for the affected time period
   - Identify affected users and requests
   - Check network policies and gateway configuration
   - Verify authentication is working correctly

2. **Investigation**:
   - Correlate logs with Prometheus metrics
   - Trace request IDs through distributed tracing
   - Check for patterns (specific user, tier, model, time period)
   - Determine if incident is isolated or widespread

3. **Mitigation**:
   - If header spoofing detected: Verify network isolation, check gateway logs
   - If billing evasion detected: Audit token counts, check vLLM logs
   - If injection attempt detected: Review sanitization code, check for bypasses
   - If DoS detected: Check Prometheus cardinality, review rate limits

4. **Recovery**:
   - Correct billing data if needed (use audit logs to reconstruct)
   - Update configuration if vulnerabilities found
   - Apply security patches
   - Enhance monitoring to detect similar incidents

5. **Post-Incident**:
   - Document incident and response
   - Review security controls
   - Update threat model if needed
   - Improve detection capabilities

### Escalation

Escalate if:
- Evidence of active exploitation
- Widespread billing fraud
- Compromise of authentication system
- Data breach or privacy violation

---

## Security Contact

For security issues or concerns, please:

1. Review this document and verify deployment follows best practices
2. Check security logs and metrics for evidence
3. Report security vulnerabilities via GitHub security advisories
4. Do NOT disclose security issues publicly until patched

---

## Appendix: Security Checklist

Use this checklist before deploying MaaS integration:

- [ ] Semantic router is NOT directly accessible by clients
- [ ] All traffic flows through MaaS gateway (Kuadrant/Authorino)
- [ ] Network policies enforce isolation
- [ ] Authentication is configured and working
- [ ] TLS is enabled for all communication
- [ ] Configuration uses safe fallback values
- [ ] Security logging is enabled
- [ ] Monitoring alerts are configured
- [ ] Incident response procedures are documented
- [ ] Team is trained on security best practices
- [ ] Regular security audits are scheduled

---

## References

- [MaaS Integration Guide](./maas-integration.md)
- [MaaS Performance Documentation](./maas-performance.md)
- [Kuadrant Documentation](https://docs.kuadrant.io/)
- [Authorino Documentation](https://github.com/Kuadrant/authorino)
- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [NIST Cybersecurity Framework](https://www.nist.gov/cyberframework)
