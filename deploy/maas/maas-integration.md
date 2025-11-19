# MaaS-Billing Integration

This document describes how semantic router integrates with [MaaS-billing](https://github.com/opendatahub-io/maas-billing) for centralized usage tracking and billing.

## Overview

MaaS-billing provides a centralized platform for tracking LLM usage and billing across multiple models and users. When integrated with semantic router, MaaS-billing handles:

- **User/tier-based billing** - Track costs per user and subscription tier
- **Token-level metering** - Precise billing based on prompt/completion tokens
- **Cache-tier pricing** - Discounted rates for cached responses
- **Security feature billing** - Track costs for PII detection and jailbreak blocking
- **Reasoning mode tracking** - Premium pricing for reasoning-enabled requests

Semantic router provides intelligent routing decisions (model selection, caching, security) while MaaS-billing handles billing and cost calculation.

## Backward Compatibility

**IMPORTANT**: MaaS integration is **disabled by default** to ensure existing deployments continue working without changes.

### For Existing Users

If you're upgrading semantic router and **not using MaaS-billing**:

- ✅ No configuration changes needed
- ✅ All existing functionality preserved
- ✅ Cost calculation continues to work as before
- ✅ Metrics remain unchanged
- ✅ No performance impact

The MaaS integration code is completely opt-in and dormant unless explicitly enabled.

### Zero-Value Safety

The code is designed to work safely when the `maas_integration` section is missing from `config.yaml`:

- Missing config section → MaaS disabled (zero value for `enabled: false`)
- All MaaS features check `IsMaasIntegrationEnabled()` before executing
- Cost calculation defaults to internal (standalone mode)
- No MaaS-specific metrics exported
- No MaaS-specific headers added

## Enabling MaaS Integration

### Prerequisites

1. **MaaS-billing deployed** - Running instance of github.com/opendatahub-io/maas-billing
2. **Gateway with authentication** - Kuadrant/Authorino gateway that sets user/tier headers
3. **Prometheus scraping** - MaaS Prometheus configured to scrape semantic router metrics

### Configuration

Add the following to your `config.yaml`:

```yaml
maas_integration:
  enabled: true

  authentication:
    user_header: "x-auth-request-user"  # Header set by MaaS gateway
    tier_header: "x-auth-request-tier"  # Header set by MaaS gateway
    fallback_user: "unknown"
    fallback_tier: "free"

  metrics:
    export_token_metrics: true
    export_cache_metrics: true
    export_routing_metrics: true
    export_security_metrics: true
    internal_cost_calculation: false    # Defer to MaaS

  headers:
    export_routing: true
    export_cache: true
    export_security: false
    prefix: "x-vsr-"
```

See `config/config.maas.yaml` for a complete reference configuration.

### Deployment Architecture

```
┌─────────┐      ┌──────────────┐      ┌──────────────────┐      ┌──────┐
│ Client  │─────>│ MaaS Gateway │─────>│ Semantic Router  │─────>│ vLLM │
└─────────┘      │ (Kuadrant)   │      │ (ExtProc Filter) │      └──────┘
                 └──────────────┘      └──────────────────┘
                        │                       │
                        │ Auth headers          │ Metrics + Headers
                        v                       v
                 ┌──────────────┐      ┌──────────────────┐
                 │ User: alice  │      │  Prometheus      │
                 │ Tier: premium│      │  (MaaS-billing)  │
                 └──────────────┘      └──────────────────┘
```

**Request Flow:**

1. Client sends request to MaaS Gateway
2. Gateway authenticates user and adds headers:
   - `x-auth-request-user: alice`
   - `x-auth-request-tier: premium`
3. Request forwarded to semantic router
4. Semantic router:
   - Extracts user/tier from headers
   - Performs intelligent routing
   - Exports metrics with user/tier labels
   - Adds routing metadata to response headers
5. MaaS-billing scrapes metrics and calculates costs

## Metrics Exported

When MaaS integration is enabled, semantic router exports these Prometheus metrics:

### Token Usage

```prometheus
semantic_router_tokens_total{user, tier, model, type}
```

- Labels: `user` (user ID), `tier` (subscription tier), `model` (selected model), `type` (prompt/completion)
- Type: Counter
- Purpose: Token-level billing per user/tier

### Request Tracking

```prometheus
semantic_router_requests_total{user, tier, model, decision}
```

- Labels: `user`, `tier`, `model`, `decision` (routing decision name)
- Type: Counter
- Purpose: Track request counts and routing decisions

### Cache Metrics

```prometheus
semantic_router_cache_hits_total{user, tier, model}
semantic_router_cache_misses_total{user, tier, model}
```

- Labels: `user`, `tier`, `model`
- Type: Counter
- Purpose: Apply cache-tier pricing (e.g., $0.01 vs $1.00 for LLM)

### Security Metrics

```prometheus
semantic_router_pii_detections_total{user, tier, pii_type}
semantic_router_jailbreak_detections_total{user, tier}
```

- Labels: `user`, `tier`, `pii_type` (for PII metric)
- Type: Counter
- Purpose: Bill for security feature usage

### Reasoning Mode Tracking

```prometheus
semantic_router_reasoning_requests_total{user, tier, model}
```

- Labels: `user`, `tier`, `model`
- Type: Counter
- Purpose: Track premium feature usage (reasoning mode has higher token costs)

## Response Headers Exported

When MaaS integration is enabled, semantic router adds these headers to responses:

### Routing Metadata

- `x-vsr-model-selected`: Model chosen by semantic router
- `x-vsr-decision`: Routing decision name (e.g., "code_generation")
- `x-vsr-category`: Domain category (e.g., "coding")
- `x-vsr-reasoning-enabled`: Whether reasoning mode was used ("on" or "off")

### Cache Information

- `x-vsr-cache-hit`: Whether response came from cache ("true" or "false")

### System Prompt Injection

- `x-vsr-system-prompt-injected`: Whether system prompt was added ("true" or "false")

**Purpose**: These headers provide MaaS-billing with context about routing decisions for accurate billing:

- **Model pricing**: Different models have different costs
- **Cache-tier pricing**: Cached responses billed at reduced rate
- **Reasoning premium**: Reasoning mode adds token overhead
- **System prompt adjustment**: Injected prompts affect token counts

## Operational Modes

### Standalone Mode (Default)

**When**: `maas_integration.enabled: false` (or section missing)

**Behavior**:
- ✅ Internal cost calculation
- ✅ Standard Prometheus metrics (no user/tier labels)
- ✅ Standard response headers
- ✅ Self-contained billing tracking

**Use cases**:
- Small deployments without MaaS infrastructure
- Development and testing
- Existing deployments (backward compatible)

### MaaS Mode

**When**: `maas_integration.enabled: true`

**Behavior**:
- ✅ Exports metrics with user/tier labels
- ✅ Adds routing metadata to response headers
- ✅ Defers cost calculation to MaaS-billing (if `internal_cost_calculation: false`)
- ✅ Extracts user/tier from authentication headers

**Use cases**:
- Production deployments with MaaS-billing
- Multi-tenant environments
- Enterprise billing requirements

### Hybrid Mode (Validation)

**When**: `maas_integration.enabled: true` and `internal_cost_calculation: true`

**Behavior**:
- ✅ Exports MaaS metrics
- ✅ Also calculates costs internally
- ✅ Both semantic router and MaaS track billing

**Use cases**:
- Migration period (validating MaaS accuracy)
- Parallel tracking for comparison
- Testing and debugging

## Migration Guide

### Step 1: Deploy MaaS-billing

Follow the [MaaS-billing installation guide](https://github.com/opendatahub-io/maas-billing) to deploy the platform.

### Step 2: Configure Authentication Gateway

Set up Kuadrant/Authorino to add authentication headers:

```yaml
# Example AuthConfig for Kuadrant
apiVersion: authorino.kuadrant.io/v1beta1
kind: AuthConfig
metadata:
  name: semantic-router-auth
spec:
  response:
    success:
      headers:
        "x-auth-request-user":
          plain:
            value: "{auth.identity.username}"
        "x-auth-request-tier":
          plain:
            value: "{auth.identity.tier}"
```

### Step 3: Enable MaaS Integration

Update `config.yaml`:

```yaml
maas_integration:
  enabled: true
  # ... other settings from config.maas.yaml
```

### Step 4: Configure Prometheus Scraping

Add semantic router as a scrape target in MaaS Prometheus:

```yaml
scrape_configs:
  - job_name: 'semantic-router'
    static_configs:
      - targets: ['semantic-router:9190']
```

### Step 5: Validate (Optional)

Enable hybrid mode temporarily to compare billing:

```yaml
maas_integration:
  enabled: true
  metrics:
    internal_cost_calculation: true  # Keep internal tracking for comparison
```

Monitor both semantic router costs and MaaS-billing to ensure accuracy.

### Step 6: Full Cutover

Disable internal cost calculation:

```yaml
maas_integration:
  enabled: true
  metrics:
    internal_cost_calculation: false  # Defer to MaaS
```

## Troubleshooting

### No user/tier in metrics

**Symptom**: Metrics show `user=unknown, tier=free` for all requests

**Causes**:
1. MaaS gateway not setting headers
2. Wrong header names configured
3. Headers not propagated through Envoy

**Solution**:
- Verify gateway AuthConfig
- Check header names in `authentication.user_header` and `authentication.tier_header`
- Enable debug logging: `export LOG_LEVEL=debug`

### Cost discrepancies

**Symptom**: MaaS billing doesn't match expected costs

**Causes**:
1. System prompt injection not accounted for
2. Cache hits billed at full rate
3. Reasoning mode overhead not included

**Solution**:
- Check `x-vsr-system-prompt-injected` header
- Verify cache-tier pricing in MaaS
- Ensure MaaS accounts for reasoning mode

### Metrics not showing up in Prometheus

**Symptom**: MaaS Prometheus doesn't have semantic router metrics

**Causes**:
1. Scraping not configured
2. Network/firewall blocking
3. Wrong port or path

**Solution**:
- Verify scrape config points to semantic-router:9190
- Check network connectivity
- Test metrics endpoint: `curl semantic-router:9190/metrics | grep semantic_router`

## Performance Impact

MaaS integration is **highly optimized** for minimal overhead:

| Operation | Overhead | Optimization |
|-----------|----------|--------------|
| Header extraction | +0.1ms | Skipped entirely when MaaS disabled |
| Metric recording | 0.15ms | Avoid duplicate metrics (50% reduction) |
| Header mutation | 0.3ms | Single config check, optimized branching |
| JSON parsing | 1-5ms | Single parse (no duplication) |
| **Total** | **0.55-5.55ms** | **+0.1ms overhead vs standalone** |

**Memory impact**: Optimized to avoid duplicate metrics
- Before: Standard + MaaS metrics = ~50% more cardinality
- After: **Only MaaS metrics** when enabled = no duplication
- Net result: ~4% memory increase (only due to user/tier labels)

**Key optimizations**:
1. ✅ Conditional metric recording - No duplicates between standard and MaaS metrics
2. ✅ Lazy header extraction - Skip when MaaS disabled
3. ✅ Optimized header mutation - Single config check, better branching
4. ✅ Inline metric recording - Avoid function call overhead

**See [Performance Documentation](./maas-performance.md) for detailed analysis and benchmarks.**

## Security Considerations

### Header Spoofing

MaaS integration trusts authentication headers set by the gateway. Ensure:

1. ✅ Semantic router is **not directly accessible** by clients
2. ✅ All traffic flows through MaaS gateway (Kuadrant/Authorino)
3. ✅ Gateway enforces authentication before setting headers
4. ✅ Network policies prevent header injection

### Metric Cardinality

User/tier labels can increase Prometheus cardinality. Mitigate by:

1. Limiting user count per tier
2. Using tier aggregation for billing
3. Setting Prometheus retention policies
4. Using metric relabeling to drop high-cardinality labels if needed

## Reference

- [MaaS-billing GitHub](https://github.com/opendatahub-io/maas-billing)
- [Kuadrant Documentation](https://docs.kuadrant.io/)
- [Authorino Documentation](https://github.com/Kuadrant/authorino)
- [Semantic Router Configuration](../config/config.maas.yaml)
