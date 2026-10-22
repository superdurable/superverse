package connectorconfiguration

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

// LocalModelHTTPClient leaves hosted clients unchanged. Only the platform's reserved local
// deployment environment enables its deterministic model service.
func (configuration Configuration) LocalModelHTTPClient() (*http.Client, error) {
	endpoint := os.Getenv("SUPERVERSE_LOCAL_MOCK_LLM_URL")
	if endpoint == "" {
		return nil, nil
	}
	broker := configuration.project.Configuration.CredentialBroker
	if os.Getenv("DEX_PROJECT_ALLOW_LOCAL_STORAGE") != "true" || broker == nil {
		return nil, fmt.Errorf("local model transport requires local project storage and the platform credential broker")
	}
	target, err := url.Parse(broker.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid platform credential broker URL")
	}
	target.Path = "/internal/mock-llm/v1/responses"
	if endpoint != target.String() || target.Scheme != "http" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, fmt.Errorf("local model endpoint must be the platform's mock route")
	}
	transport := &http.Transport{MaxConnsPerHost: 8, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
	return &http.Client{Transport: localModelTransport{target: *target, transport: transport}, Timeout: 30 * time.Second}, nil
}

type localModelTransport struct {
	target    url.URL
	transport *http.Transport
}

// RoundTrip never forwards a provider key, follows a redirect or falls back to a
// real provider. The normal connector still encodes and decodes the HTTP exchange.
func (t localModelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.String() != "https://api.openai.com/v1/responses" || request.Header.Get("Authorization") != "Bearer superverse-local-mock-llm-not-a-secret" {
		return nil, fmt.Errorf("local model transport accepts only synthetic OpenAI summary requests")
	}
	clone := request.Clone(request.Context())
	target := t.target
	clone.URL, clone.Host = &target, target.Host
	return t.transport.RoundTrip(clone)
}
