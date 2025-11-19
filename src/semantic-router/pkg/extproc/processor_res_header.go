package extproc

import (
	"strconv"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	http_ext "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/headers"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/metrics"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/utils/security"
)

// handleResponseHeaders processes the response headers
func (r *OpenAIRouter) handleResponseHeaders(v *ext_proc.ProcessingRequest_ResponseHeaders, ctx *RequestContext) (*ext_proc.ProcessingResponse, error) {
	var statusCode int
	var isSuccessful bool

	// Detect upstream HTTP status and record non-2xx as errors
	if v != nil && v.ResponseHeaders != nil && v.ResponseHeaders.Headers != nil {
		// Determine if the response is streaming based on Content-Type
		ctx.IsStreamingResponse = isStreamingContentType(v.ResponseHeaders.Headers)

		statusCode = getStatusFromHeaders(v.ResponseHeaders.Headers)
		isSuccessful = statusCode >= 200 && statusCode < 300

		if statusCode != 0 {
			if statusCode >= 500 {
				metrics.RecordRequestError(getModelFromCtx(ctx), "upstream_5xx")
			} else if statusCode >= 400 {
				metrics.RecordRequestError(getModelFromCtx(ctx), "upstream_4xx")
			}
		}
	}

	// Best-effort TTFT measurement:
	// - For non-streaming responses, record on first response headers (approx TTFB ~= TTFT)
	// - For streaming responses (SSE), defer TTFT until the first response body chunk arrives
	if ctx != nil && !ctx.IsStreamingResponse && !ctx.TTFTRecorded && !ctx.ProcessingStartTime.IsZero() && ctx.RequestModel != "" {
		ttft := time.Since(ctx.ProcessingStartTime).Seconds()
		if ttft > 0 {
			metrics.RecordModelTTFT(ctx.RequestModel, ttft)
			ctx.TTFTSeconds = ttft
			ctx.TTFTRecorded = true
		}
	}

	// Prepare response headers with VSR decision tracking headers if applicable
	var headerMutation *ext_proc.HeaderMutation

	// Add VSR decision headers if request was successful and didn't hit cache
	if isSuccessful && !ctx.VSRCacheHit && ctx != nil {
		var setHeaders []*core.HeaderValueOption

		// Optimization: Check config once and cache decisions
		isMaasEnabled := r.Config != nil && r.Config.IsMaasIntegrationEnabled()
		shouldAddRoutingHeaders := true
		var prefix string

		if isMaasEnabled {
			// MaaS mode: Check if we should export headers and get prefix
			shouldAddRoutingHeaders = r.Config.ShouldExportRoutingHeaders()
			if shouldAddRoutingHeaders {
				prefix = r.Config.GetMaasHeaderPrefix()
			}
		}
		// Standalone mode: always add headers, no prefix

		if shouldAddRoutingHeaders {

			// Optimization: Build headers based on mode (avoid repeated prefix checks)
			if prefix != "" {
				// MaaS mode: Use custom prefix
				if ctx.VSRSelectedCategory != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedCategory, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedCategory)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      prefix + "category",
							RawValue: []byte(sanitizedCategory),
						},
					})
				}

				if ctx.VSRSelectedDecisionName != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedDecision, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedDecisionName)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      prefix + "decision",
							RawValue: []byte(sanitizedDecision),
						},
					})
				}

				if ctx.VSRReasoningMode != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedReasoning, _ := security.SanitizeHTTPHeader(ctx.VSRReasoningMode)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      prefix + "reasoning-enabled",
							RawValue: []byte(sanitizedReasoning),
						},
					})
				}

				if ctx.VSRSelectedModel != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedModel, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedModel)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      prefix + "model-selected",
							RawValue: []byte(sanitizedModel),
						},
					})
				}

				injectedValue := "false"
				if ctx.VSRInjectedSystemPrompt {
					injectedValue = "true"
				}
				setHeaders = append(setHeaders, &core.HeaderValueOption{
					Header: &core.HeaderValue{
						Key:      prefix + "system-prompt-injected",
						RawValue: []byte(injectedValue),
					},
				})
			} else {
				// Standalone mode: Use standard header names (no prefix)
				if ctx.VSRSelectedCategory != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedCategory, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedCategory)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      headers.VSRSelectedCategory,
							RawValue: []byte(sanitizedCategory),
						},
					})
				}

				if ctx.VSRSelectedDecisionName != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedDecision, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedDecisionName)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      headers.VSRSelectedDecision,
							RawValue: []byte(sanitizedDecision),
						},
					})
				}

				if ctx.VSRReasoningMode != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedReasoning, _ := security.SanitizeHTTPHeader(ctx.VSRReasoningMode)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      headers.VSRSelectedReasoning,
							RawValue: []byte(sanitizedReasoning),
						},
					})
				}

				if ctx.VSRSelectedModel != "" {
					// Security: Sanitize header value to prevent CRLF injection
					sanitizedModel, _ := security.SanitizeHTTPHeader(ctx.VSRSelectedModel)
					setHeaders = append(setHeaders, &core.HeaderValueOption{
						Header: &core.HeaderValue{
							Key:      headers.VSRSelectedModel,
							RawValue: []byte(sanitizedModel),
						},
					})
				}

				injectedValue := "false"
				if ctx.VSRInjectedSystemPrompt {
					injectedValue = "true"
				}
				setHeaders = append(setHeaders, &core.HeaderValueOption{
					Header: &core.HeaderValue{
						Key:      headers.VSRInjectedSystemPrompt,
						RawValue: []byte(injectedValue),
					},
				})
			}
		}

		// Add cache hit header if MaaS integration is enabled and cache export is configured
		// Optimization: Reuse isMaasEnabled check from above
		if isMaasEnabled && r.Config.ShouldExportCacheHeaders() {
			// Reuse prefix from above if available, otherwise get it
			if prefix == "" {
				prefix = r.Config.GetMaasHeaderPrefix()
			}
			cacheHitValue := "false"
			if ctx.VSRCacheHit {
				cacheHitValue = "true"
			}
			setHeaders = append(setHeaders, &core.HeaderValueOption{
				Header: &core.HeaderValue{
					Key:      prefix + "cache-hit",
					RawValue: []byte(cacheHitValue),
				},
			})
		}

		// Create header mutation if we have headers to add
		if len(setHeaders) > 0 {
			headerMutation = &ext_proc.HeaderMutation{
				SetHeaders: setHeaders,
			}
		}
	}

	// Allow the response to continue with VSR headers if applicable
	response := &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &ext_proc.HeadersResponse{
				Response: &ext_proc.CommonResponse{
					Status:         ext_proc.CommonResponse_CONTINUE,
					HeaderMutation: headerMutation,
				},
			},
		},
	}

	// If this is a streaming (SSE) response, instruct Envoy to stream the response body to ExtProc
	// so we can capture TTFT on the first body chunk. Requires allow_mode_override: true in Envoy config.
	if ctx != nil && ctx.IsStreamingResponse {
		response.ModeOverride = &http_ext.ProcessingMode{
			ResponseBodyMode: http_ext.ProcessingMode_STREAMED,
		}
	}

	return response, nil
}

// getStatusFromHeaders extracts :status pseudo-header value as integer
func getStatusFromHeaders(headerMap *core.HeaderMap) int {
	if headerMap == nil {
		return 0
	}
	for _, hv := range headerMap.Headers {
		if hv.Key == ":status" {
			if hv.Value != "" {
				if code, err := strconv.Atoi(hv.Value); err == nil {
					return code
				}
			}
			if len(hv.RawValue) > 0 {
				if code, err := strconv.Atoi(string(hv.RawValue)); err == nil {
					return code
				}
			}
		}
	}
	return 0
}

func getModelFromCtx(ctx *RequestContext) string {
	if ctx == nil || ctx.RequestModel == "" {
		return "unknown"
	}
	return ctx.RequestModel
}

// isStreamingContentType checks if the response content-type indicates streaming (SSE)
func isStreamingContentType(headerMap *core.HeaderMap) bool {
	if headerMap == nil {
		return false
	}
	for _, hv := range headerMap.Headers {
		if strings.ToLower(hv.Key) == "content-type" {
			val := hv.Value
			if val == "" && len(hv.RawValue) > 0 {
				val = string(hv.RawValue)
			}
			if strings.Contains(strings.ToLower(val), "text/event-stream") {
				return true
			}
		}
	}
	return false
}
