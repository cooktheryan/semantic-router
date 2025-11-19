# MaaS-Billing Integration for Semantic Router

This directory contains configuration and documentation for deploying semantic router with [MaaS-billing](https://github.com/opendatahub-io/maas-billing) integration for centralized LLM usage tracking and billing.

## Quick Deployment

### Prerequisites

- MaaS-billing deployed and running
- Kuadrant/Authorino gateway configured
- Prometheus configured to scrape semantic router metrics (port 9190)
- OpenShift or Kubernetes cluster access

### Deploy with MaaS Integration

```bash
# Copy reference configuration
cp deploy/maas/config.maas.yaml config/config.yaml

# Edit config.yaml and set maas_integration.enabled: true

# Build and deploy
make build
make run-router
```

## What MaaS Integration Provides

- ✅ User/tier-based billing tracking
- ✅ Token-level metering (prompt/completion)
- ✅ Cache-tier pricing for cached responses
- ✅ Security feature tracking (PII, jailbreak detection)
- ✅ Reasoning mode usage tracking

## Architecture

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

## Configuration

### Enable MaaS Integration

Edit `config.yaml`:

```yaml
maas_integration:
  enabled: true

  authentication:
    user_header: "x-auth-request-user"
    tier_header: "x-auth-request-tier"
    fallback_user: "unknown"
    fallback_tier: "free"

  metrics:
    export_token_metrics: true
    export_cache_metrics: true
    export_routing_metrics: true
    export_security_metrics: true
    internal_cost_calculation: false

  headers:
    export_routing: true
    export_cache: true
    export_security: false
    prefix: "x-vsr-"
```

### Configure Gateway Authentication

Create Kuadrant AuthConfig:

```yaml
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

### Configure Prometheus Scraping

Add to MaaS Prometheus config:

```yaml
scrape_configs:
  - job_name: 'semantic-router'
    static_configs:
      - targets: ['semantic-router:9190']
```

## Deployment

### Local Deployment

```bash
# Terminal 1: Start Envoy
make run-envoy

# Terminal 2: Start router with MaaS config
CONFIG_FILE=config/config.yaml make run-router
```

### Kubernetes Deployment

```bash
# Create ConfigMap with MaaS config
kubectl create configmap semantic-router-config \
  --from-file=config.yaml=deploy/maas/config.maas.yaml

# Deploy
kubectl apply -f deploy/kubernetes/
```

### OpenShift Deployment

```bash
# Create ConfigMap with MaaS config
oc create configmap semantic-router-config \
  --from-file=config.yaml=deploy/maas/config.maas.yaml

# Deploy
oc apply -f deploy/openshift/
```

## Verification

### Check MaaS Integration Status

```bash
# Check logs for MaaS initialization
kubectl logs -f deployment/semantic-router | grep -i maas
```

### Verify Metrics with User/Tier Labels

```bash
# Check metrics endpoint
curl http://semantic-router:9190/metrics | grep semantic_router_tokens_total

# Expected output:
# semantic_router_tokens_total{user="alice",tier="premium",model="qwen3",type="prompt"} 1500
```

### Verify Response Headers

```bash
# Send test request
curl -v -X POST http://semantic-router:8801/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "x-auth-request-user: testuser" \
  -H "x-auth-request-tier: free" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "What is 2+2?"}]}'

# Expected headers in response:
# x-vsr-model-selected: qwen3
# x-vsr-decision: math
# x-vsr-category: math
# x-vsr-reasoning-enabled: off
# x-vsr-cache-hit: false
```

### Verify MaaS Prometheus Has Metrics

```bash
# Query MaaS Prometheus
curl http://maas-prometheus:9090/api/v1/query?query=semantic_router_tokens_total
```

## Operational Modes

| Mode | When | Behavior |
|------|------|----------|
| **Standalone** (default) | `maas_integration.enabled: false` | Internal cost calculation, standard metrics, no user/tier labels |
| **MaaS** | `maas_integration.enabled: true` | Exports metrics with user/tier labels, defers cost calculation to MaaS |
| **Hybrid** | `enabled: true` + `internal_cost_calculation: true` | Both MaaS metrics and internal cost calculation (for validation) |

## Troubleshooting

### Metrics show user=unknown, tier=free

```bash
# Check gateway is setting headers
oc logs deployment/kuadrant-gateway | grep x-auth-request

# Verify header names in config match gateway
grep -A 5 "authentication:" config/config.yaml

# Enable debug logging
export LOG_LEVEL=debug
```

### MaaS Prometheus not showing metrics

```bash
# Verify scrape target
curl http://maas-prometheus:9090/api/v1/targets | grep semantic-router

# Check network connectivity
kubectl exec -it deployment/maas-prometheus -- curl http://semantic-router:9190/metrics

# Test metrics endpoint directly
curl semantic-router:9190/metrics | grep semantic_router
```

### Cost discrepancies in MaaS billing

```bash
# Check response headers for routing context
curl -v http://semantic-router:8801/v1/chat/completions ... | grep x-vsr

# Verify system prompt injection (adds tokens)
grep "x-vsr-system-prompt-injected"

# Check if reasoning mode is enabled (higher token cost)
grep "x-vsr-reasoning-enabled"
```

## Performance Impact

MaaS integration overhead: **~0.55ms per request**

| Operation | Latency |
|-----------|---------|
| Header extraction | 0.1ms |
| Metric recording | 0.15ms |
| Header mutation | 0.3ms |

Memory impact: ~4% increase (user/tier labels)

See [maas-performance.md](./maas-performance.md) for detailed benchmarks.

## Security Considerations

**IMPORTANT**: Semantic router must not be directly accessible by clients.

- ✅ All traffic through MaaS gateway (Kuadrant/Authorino)
- ✅ Gateway enforces authentication before setting headers
- ✅ Network policies prevent header injection
- ✅ Semantic router only accessible from gateway

See [maas-security.md](./maas-security.md) for security architecture.

## Backward Compatibility

**MaaS integration is disabled by default.**

Existing deployments upgrading semantic router:
- ✅ No configuration changes required
- ✅ All functionality preserved
- ✅ No performance impact when disabled
- ✅ Completely opt-in

See [maas-backward-compatibility.md](./maas-backward-compatibility.md) for verification.

## Files in this Directory

- `config.maas.yaml` - Reference configuration with MaaS integration
- `maas-integration.md` - Comprehensive integration guide
- `maas-performance.md` - Performance analysis and benchmarks
- `maas-security.md` - Security architecture and threat model
- `maas-backward-compatibility.md` - Backward compatibility verification

## Additional Resources

- [MaaS-billing](https://github.com/opendatahub-io/maas-billing)
- [Kuadrant Documentation](https://docs.kuadrant.io/)
- [Authorino Documentation](https://github.com/Kuadrant/authorino)
