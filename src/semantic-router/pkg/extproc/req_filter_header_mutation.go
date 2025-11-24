package extproc

import (
	"fmt"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/config"
	"github.com/vllm-project/semantic-router/src/semantic-router/pkg/observability/logging"
)

// buildHeaderMutations builds header mutations based on the decision's header_mutation plugin configuration
// Returns (setHeaders, removeHeaders) to be applied to the request
func (r *OpenAIRouter) buildHeaderMutations(decision *config.Decision) ([]*corev3.HeaderValueOption, []string) {
	if decision == nil {
		return nil, nil
	}

	// Get header mutation configuration
	headerConfig := decision.GetHeaderMutationConfig()
	if headerConfig == nil {
		return nil, nil
	}

	logging.Debugf("Building header mutations for decision %s: add=%d, update=%d, delete=%d",
		decision.Name, len(headerConfig.Add), len(headerConfig.Update), len(headerConfig.Delete))

	var setHeaders []*corev3.HeaderValueOption
	var removeHeaders []string

	// Apply additions (add new headers)
	for _, headerPair := range headerConfig.Add {
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:      headerPair.Name,
				RawValue: []byte(headerPair.Value),
			},
		})
		logging.Debugf("Adding header: %s=%s", headerPair.Name, headerPair.Value)
	}

	// Apply updates (modify existing headers - in Envoy this is the same as set)
	for _, headerPair := range headerConfig.Update {
		setHeaders = append(setHeaders, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:      headerPair.Name,
				RawValue: []byte(headerPair.Value),
			},
		})
		logging.Debugf("Updating header: %s=%s", headerPair.Name, headerPair.Value)
	}

	// Apply deletions
	for _, headerName := range headerConfig.Delete {
		removeHeaders = append(removeHeaders, headerName)
		logging.Debugf("Deleting header: %s", headerName)
	}

	return setHeaders, removeHeaders
}

// buildMaaSAuthHeader builds the Authorization header for MaaS requests
// Returns the header and any error encountered during token acquisition
func (r *OpenAIRouter) buildMaaSAuthHeader() (*corev3.HeaderValueOption, error) {
	// Check if MaaS is enabled
	if !r.Config.IsMaaSEnabled() {
		return nil, nil
	}

	// Check if token manager is initialized
	if r.MaaSTokenManager == nil {
		return nil, fmt.Errorf("MaaS is enabled but token manager is not initialized")
	}

	// Acquire MaaS token
	token, err := r.MaaSTokenManager.GetToken()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire MaaS token: %w", err)
	}

	// Build Authorization header
	authHeader := &corev3.HeaderValueOption{
		Header: &corev3.HeaderValue{
			Key:      "authorization",
			RawValue: []byte(fmt.Sprintf("Bearer %s", token)),
		},
	}

	logging.Debugf("Injecting MaaS Authorization header (token length: %d)", len(token))
	return authHeader, nil
}
