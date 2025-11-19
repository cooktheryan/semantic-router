package extproc

import (
	"encoding/json"
	"time"

	ext_proc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/openai/openai-go"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/metrics"
)

// handleResponseBody processes the response body
func (r *OpenAIRouter) handleResponseBody(v *ext_proc.ProcessingRequest_ResponseBody, ctx *RequestContext) (*ext_proc.ProcessingResponse, error) {
	completionLatency := time.Since(ctx.StartTime)

	// Process the response for caching
	responseBody := v.ResponseBody.Body

	// If this is a streaming response (e.g., SSE), record TTFT on the first body chunk
	// and skip JSON parsing/caching which are not applicable for SSE chunks.
	if ctx.IsStreamingResponse {
		if ctx != nil && !ctx.TTFTRecorded && !ctx.ProcessingStartTime.IsZero() && ctx.RequestModel != "" {
			ttft := time.Since(ctx.ProcessingStartTime).Seconds()
			if ttft > 0 {
				metrics.RecordModelTTFT(ctx.RequestModel, ttft)
				ctx.TTFTSeconds = ttft
				ctx.TTFTRecorded = true
				logging.Infof("Recorded TTFT on first streamed body chunk: %.3fs", ttft)
			}
		}

		// For streaming chunks, just continue (no token parsing or cache update)
		response := &ext_proc.ProcessingResponse{
			Response: &ext_proc.ProcessingResponse_ResponseBody{
				ResponseBody: &ext_proc.BodyResponse{
					Response: &ext_proc.CommonResponse{
						Status: ext_proc.CommonResponse_CONTINUE,
					},
				},
			},
		}
		return response, nil
	}

	// Parse tokens from the response JSON using OpenAI SDK types
	var parsed openai.ChatCompletion
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		logging.Errorf("Error parsing tokens from response: %v", err)
		metrics.RecordRequestError(ctx.RequestModel, "parse_error")
	}
	promptTokens := int(parsed.Usage.PromptTokens)
	completionTokens := int(parsed.Usage.CompletionTokens)

	// Security: Validate token counts to prevent billing evasion
	// Token counts should be non-negative and reasonable
	if promptTokens < 0 || completionTokens < 0 {
		logging.Warnf("Security: Negative token counts detected (possible billing evasion): prompt=%d, completion=%d - setting to 0",
			promptTokens, completionTokens)
		metrics.RecordRequestError(ctx.RequestModel, "invalid_token_count")
		if promptTokens < 0 {
			promptTokens = 0
		}
		if completionTokens < 0 {
			completionTokens = 0
		}
	}

	// Security: Log if token counts are zero (possible JSON parsing error or billing evasion)
	if promptTokens == 0 && completionTokens == 0 && len(responseBody) > 0 {
		logging.Warnf("Security: Zero token counts detected for non-empty response (possible billing evasion or parsing error): request_id=%s, model=%s",
			ctx.RequestID, ctx.RequestModel)
		metrics.RecordRequestError(ctx.RequestModel, "zero_token_count")
	}

	// Security: Validate token counts are reasonable (prevent absurdly large values)
	const maxReasonableTokens = 1_000_000 // 1M tokens is very large
	if promptTokens > maxReasonableTokens || completionTokens > maxReasonableTokens {
		logging.Warnf("Security: Unusually large token counts detected (possible billing manipulation): prompt=%d, completion=%d, max=%d",
			promptTokens, completionTokens, maxReasonableTokens)
		metrics.RecordRequestError(ctx.RequestModel, "excessive_token_count")
		// Don't cap the tokens - this could be legitimate, just log for investigation
	}

	// Record tokens used with the model that was used
	if ctx.RequestModel != "" {
		// Optimization: Avoid duplicate metrics - only record MaaS metrics when MaaS is enabled
		// MaaS metrics contain superset of information (user/tier/model vs just model)
		isMaasEnabled := r.Config != nil && r.Config.IsMaasIntegrationEnabled()

		if isMaasEnabled {
			// MaaS mode: Record metrics with user/tier labels (more granular)
			if r.Config.ShouldExportTokenMetrics() {
				metrics.RecordMaasTokens(ctx.MaasUser, ctx.MaasTier, ctx.RequestModel, "prompt", float64(promptTokens))
				metrics.RecordMaasTokens(ctx.MaasUser, ctx.MaasTier, ctx.RequestModel, "completion", float64(completionTokens))
			}

			if r.Config.ShouldExportRoutingMetrics() {
				metrics.RecordMaasRequest(ctx.MaasUser, ctx.MaasTier, ctx.RequestModel, ctx.VSRSelectedDecisionName)
			}

			if ctx.VSRReasoningMode == "on" {
				metrics.RecordMaasReasoningRequest(ctx.MaasUser, ctx.MaasTier, ctx.RequestModel)
			}
		} else {
			// Standalone mode: Record standard metrics (backward compatible)
			metrics.RecordModelTokensDetailed(
				ctx.RequestModel,
				float64(promptTokens),
				float64(completionTokens),
			)
		}

		// Always record latency metrics (operational, not billing-related)
		metrics.RecordModelCompletionLatency(ctx.RequestModel, completionLatency.Seconds())

		// Record TPOT (time per output token) if completion tokens are available
		if completionTokens > 0 {
			timePerToken := completionLatency.Seconds() / float64(completionTokens)
			metrics.RecordModelTPOT(ctx.RequestModel, timePerToken)
		}

		// Compute and record cost if pricing is configured (conditional based on MaaS config)
		if r.Config != nil && r.Config.ShouldCalculateCostsInternally() {
			promptRatePer1M, completionRatePer1M, currency, ok := r.Config.GetModelPricing(ctx.RequestModel)
			if ok {
				costAmount := (float64(promptTokens)*promptRatePer1M + float64(completionTokens)*completionRatePer1M) / 1_000_000.0
				if currency == "" {
					currency = "USD"
				}
				metrics.RecordModelCost(ctx.RequestModel, currency, costAmount)
				logging.LogEvent("llm_usage", map[string]interface{}{
					"request_id":            ctx.RequestID,
					"model":                 ctx.RequestModel,
					"prompt_tokens":         promptTokens,
					"completion_tokens":     completionTokens,
					"total_tokens":          promptTokens + completionTokens,
					"completion_latency_ms": completionLatency.Milliseconds(),
					"cost":                  costAmount,
					"currency":              currency,
				})
			} else {
				logging.LogEvent("llm_usage", map[string]interface{}{
					"request_id":            ctx.RequestID,
					"model":                 ctx.RequestModel,
					"prompt_tokens":         promptTokens,
					"completion_tokens":     completionTokens,
					"total_tokens":          promptTokens + completionTokens,
					"completion_latency_ms": completionLatency.Milliseconds(),
					"cost":                  0.0,
					"currency":              "unknown",
					"pricing":               "not_configured",
				})
			}
		} else if r.Config != nil && r.Config.IsMaasIntegrationEnabled() {
			// MaaS mode: log usage without cost (cost calculation deferred to MaaS-billing)
			logging.LogEvent("llm_usage", map[string]interface{}{
				"request_id":            ctx.RequestID,
				"model":                 ctx.RequestModel,
				"prompt_tokens":         promptTokens,
				"completion_tokens":     completionTokens,
				"total_tokens":          promptTokens + completionTokens,
				"completion_latency_ms": completionLatency.Milliseconds(),
				"maas_user":             ctx.MaasUser,
				"maas_tier":             ctx.MaasTier,
				"billing_mode":          "maas",
			})
		}
	}

	// Update the cache
	if ctx.RequestID != "" && responseBody != nil {
		err := r.Cache.UpdateWithResponse(ctx.RequestID, responseBody)
		if err != nil {
			logging.Errorf("Error updating cache: %v", err)
			// Continue even if cache update fails
		} else {
			logging.Infof("Cache updated for request ID: %s", ctx.RequestID)
		}
	}

	// Allow the response to continue without modification
	response := &ext_proc.ProcessingResponse{
		Response: &ext_proc.ProcessingResponse_ResponseBody{
			ResponseBody: &ext_proc.BodyResponse{
				Response: &ext_proc.CommonResponse{
					Status: ext_proc.CommonResponse_CONTINUE,
				},
			},
		},
	}

	return response, nil
}
