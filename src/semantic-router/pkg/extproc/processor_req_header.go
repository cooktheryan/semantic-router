package extproc

import (
	"context"
	"strings"
	"time"

	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/config"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/headers"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/tracing"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/utils/security"
)

// RequestContext holds the context for processing a request
type RequestContext struct {
	Headers             map[string]string
	RequestID           string
	OriginalRequestBody []byte
	RequestModel        string
	RequestQuery        string
	StartTime           time.Time
	ProcessingStartTime time.Time

	// Streaming detection
	ExpectStreamingResponse bool // set from request Accept header or stream parameter
	IsStreamingResponse     bool // set from response Content-Type

	// TTFT tracking
	TTFTRecorded bool
	TTFTSeconds  float64

	// VSR decision tracking
	VSRSelectedCategory     string           // The category from domain classification (MMLU category)
	VSRSelectedDecisionName string           // The decision name from DecisionEngine evaluation
	VSRReasoningMode        string           // "on" or "off" - whether reasoning mode was determined to be used
	VSRSelectedModel        string           // The model selected by VSR
	VSRCacheHit             bool             // Whether this request hit the cache
	VSRInjectedSystemPrompt bool             // Whether a system prompt was injected into the request
	VSRSelectedDecision     *config.Decision // The decision object selected by DecisionEngine (for plugins)

	// MaaS-billing integration (user/tier for metrics and billing)
	MaasUser string // User identity extracted from auth headers (e.g., "x-auth-request-user")
	MaasTier string // User tier extracted from auth headers (e.g., "x-auth-request-tier")

	// Tracing context
	TraceContext context.Context // OpenTelemetry trace context for span propagation
}

// handleRequestHeaders processes the request headers
func (r *OpenAIRouter) handleRequestHeaders(v *ext_proc.ProcessingRequest_RequestHeaders, ctx *RequestContext) (*ext_proc.ProcessingResponse, error) {
	// Record start time for overall request processing
	ctx.StartTime = time.Now()

	// Initialize trace context from incoming headers
	baseCtx := context.Background()
	headerMap := make(map[string]string)
	for _, h := range v.RequestHeaders.Headers.Headers {
		headerValue := h.Value
		if headerValue == "" && len(h.RawValue) > 0 {
			headerValue = string(h.RawValue)
		}
		headerMap[h.Key] = headerValue
	}

	// Extract trace context from headers (if present)
	ctx.TraceContext = tracing.ExtractTraceContext(baseCtx, headerMap)

	// Start root span for the request
	spanCtx, span := tracing.StartSpan(ctx.TraceContext, tracing.SpanRequestReceived,
		trace.WithSpanKind(trace.SpanKindServer))
	ctx.TraceContext = spanCtx
	defer span.End()

	// Store headers for later use
	requestHeaders := v.RequestHeaders.Headers
	logging.Debugf("Processing request headers: %+v", requestHeaders.Headers)
	for _, h := range requestHeaders.Headers {
		// Prefer Value when available; fall back to RawValue
		headerValue := h.Value
		if headerValue == "" && len(h.RawValue) > 0 {
			headerValue = string(h.RawValue)
		}

		ctx.Headers[h.Key] = headerValue
		// Store request ID if present (case-insensitive)
		if strings.ToLower(h.Key) == headers.RequestID {
			ctx.RequestID = headerValue
		}
	}

	// Extract MaaS user/tier from headers if MaaS integration is enabled
	if r.Config != nil && r.Config.IsMaasIntegrationEnabled() {
		// Extract user identity
		userHeader := r.Config.GetMaasUserHeader()
		if user, ok := ctx.Headers[userHeader]; ok && user != "" {
			// Security: Validate and sanitize user header to prevent spoofing/injection
			if trusted, reason := security.ValidateTrustedHeader(userHeader, user); !trusted {
				logging.Warnf("Untrusted user header detected (%s): %s - using fallback", reason, user)
				ctx.MaasUser = r.Config.GetMaasFallbackUser()
			} else {
				// Sanitize for safe use in Prometheus labels
				sanitizedUser, modified := security.SanitizeMaasUser(user)
				if modified {
					logging.Warnf("Sanitized MaaS user header from '%s' to '%s' for security", user, sanitizedUser)
				}
				ctx.MaasUser = sanitizedUser
				logging.Debugf("Extracted MaaS user from header %s: %s", userHeader, sanitizedUser)
			}
		} else {
			ctx.MaasUser = r.Config.GetMaasFallbackUser()
			logging.Debugf("Using fallback MaaS user: %s", ctx.MaasUser)
		}

		// Extract user tier
		tierHeader := r.Config.GetMaasTierHeader()
		if tier, ok := ctx.Headers[tierHeader]; ok && tier != "" {
			// Security: Validate and sanitize tier header to prevent spoofing/injection
			if trusted, reason := security.ValidateTrustedHeader(tierHeader, tier); !trusted {
				logging.Warnf("Untrusted tier header detected (%s): %s - using fallback", reason, tier)
				ctx.MaasTier = r.Config.GetMaasFallbackTier()
			} else {
				// Sanitize for safe use in Prometheus labels
				sanitizedTier, modified := security.SanitizeMaasTier(tier)
				if modified {
					logging.Warnf("Sanitized MaaS tier header from '%s' to '%s' for security", tier, sanitizedTier)
				}
				ctx.MaasTier = sanitizedTier
				logging.Debugf("Extracted MaaS tier from header %s: %s", tierHeader, sanitizedTier)
			}
		} else {
			ctx.MaasTier = r.Config.GetMaasFallbackTier()
			logging.Debugf("Using fallback MaaS tier: %s", ctx.MaasTier)
		}

		// Add user/tier to trace span attributes
		tracing.SetSpanAttributes(span,
			attribute.String("maas.user", ctx.MaasUser),
			attribute.String("maas.tier", ctx.MaasTier))
	}

	// Set request metadata on span
	if ctx.RequestID != "" {
		tracing.SetSpanAttributes(span,
			attribute.String(tracing.AttrRequestID, ctx.RequestID))
	}

	method := ctx.Headers[":method"]
	path := ctx.Headers[":path"]
	tracing.SetSpanAttributes(span,
		attribute.String(tracing.AttrHTTPMethod, method),
		attribute.String(tracing.AttrHTTPPath, path))

	// Detect if the client expects a streaming response (SSE)
	if accept, ok := ctx.Headers["accept"]; ok {
		if strings.Contains(strings.ToLower(accept), "text/event-stream") {
			ctx.ExpectStreamingResponse = true
			logging.Infof("Client expects streaming response based on Accept header")
		}
	}

	// Check if this is a GET request to /v1/models
	if method == "GET" && strings.HasPrefix(path, "/v1/models") {
		logging.Infof("Handling /v1/models request with path: %s", path)
		return r.handleModelsRequest(path)
	}

	// Prepare base response
	response := &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_RequestHeaders{
			RequestHeaders: &ext_proc.HeadersResponse{
				Response: &ext_proc.CommonResponse{
					Status: ext_proc.CommonResponse_CONTINUE,
					// No HeaderMutation - will be handled in body phase
				},
			},
		},
	}

	// If streaming is expected, we rely on Envoy config to set response_body_mode: STREAMED for SSE.
	// Some Envoy/control-plane versions may not support per-message ModeOverride; avoid compile-time coupling here.
	// The Accept header is still recorded on context for downstream logic.

	return response, nil
}
