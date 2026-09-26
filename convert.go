package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// statusTokenCodes maps Envoy retry_on tokens that describe HTTP responses
// to the concrete status codes the AWS SDK's retryer would need instead.
// Envoy's "5xx" and "gateway-error" are wildcards; AWS retry config has no
// wildcard, so we expand them to the status codes the AWS SDK itself
// treats as retryable by default.
var statusTokenCodes = map[string][]int{
	"5xx":           {500, 502, 503, 504},
	"gateway-error": {502, 503, 504},
	"retriable-4xx": {409},
}

// unmappableTokens are Envoy retry_on values that describe something below
// the HTTP response (a connection reset, a gRPC status, a stream refusal).
// AWS retry config only ever keys off a response status code, so these
// simply have nowhere to go.
var unmappableTokens = map[string]bool{
	"reset":                      true,
	"connect-failure":            true,
	"refused-stream":             true,
	"envoy-ratelimited":          true,
	"reset-before-request":       true,
	"http3-post-connect-failure": true,
	"cancelled":                  true,
	"deadline-exceeded":          true,
	"resource-exhausted":         true,
	"unavailable":                true,
}

// EnvoyToAWS converts an Envoy/Istio retry policy to the AWS SDK retry
// config shape. It returns any non-fatal warnings produced along the way
// (only possible when lenient is true; in strict mode the same conditions
// are returned as errors instead).
func EnvoyToAWS(p *EnvoyRetryPolicy, lenient bool) (*AWSRetryPolicy, []string, error) {
	var warnings []string

	attempts := 2 // Envoy's documented default is 1 retry, i.e. 2 attempts.
	if p.NumRetries != nil {
		n := *p.NumRetries
		if n < 0 {
			if !lenient {
				return nil, nil, fmt.Errorf("num_retries is negative (%d); rerun with --lenient to clamp it to 0", n)
			}
			warnings = append(warnings, fmt.Sprintf("num_retries %d clamped to 0", n))
			n = 0
		}
		attempts = n + 1
	}

	if p.PerTryTimeout != "" {
		if !lenient {
			return nil, nil, fmt.Errorf("per_try_timeout has no equivalent in the aws retry format; rerun with --lenient to drop it")
		}
		warnings = append(warnings, fmt.Sprintf("dropped per_try_timeout %q: not representable in aws retry config", p.PerTryTimeout))
	}

	codes, codeWarnings, err := retryOnToStatusCodes(p.RetryOn, lenient)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, codeWarnings...)

	backoff, err := convertBackoff(p.RetryBackOff)
	if err != nil {
		return nil, nil, err
	}

	out := &AWSRetryPolicy{
		MaxAttempts:          attempts,
		Mode:                 "standard",
		Backoff:              backoff,
		RetryableStatusCodes: codes,
	}
	return out, warnings, nil
}

func retryOnToStatusCodes(retryOn string, lenient bool) ([]int, []string, error) {
	var warnings []string
	var unknown []string
	seen := map[int]bool{}
	var codes []int

	for _, raw := range strings.Split(retryOn, ",") {
		token := strings.ToLower(strings.TrimSpace(raw))
		if token == "" {
			continue
		}
		if mapped, ok := statusTokenCodes[token]; ok {
			for _, c := range mapped {
				if !seen[c] {
					seen[c] = true
					codes = append(codes, c)
				}
			}
			continue
		}
		if unmappableTokens[token] {
			unknown = append(unknown, token)
			continue
		}
		unknown = append(unknown, token+" (unrecognized)")
	}

	if len(unknown) > 0 {
		if !lenient {
			return nil, nil, fmt.Errorf("retry_on token(s) with no status-code equivalent: %s (rerun with --lenient to drop them)", strings.Join(unknown, ", "))
		}
		warnings = append(warnings, fmt.Sprintf("dropped retry_on token(s) with no status-code equivalent: %s", strings.Join(unknown, ", ")))
	}

	sort.Ints(codes)
	return codes, warnings, nil
}

func convertBackoff(b *EnvoyRetryBackOff) (*AWSBackoff, error) {
	if b == nil {
		return nil, nil
	}
	base, err := parseDurationMS("base_interval", b.BaseInterval)
	if err != nil {
		return nil, err
	}
	// Envoy defaults max_interval to 10x base_interval when unset.
	maxMS := base * 10
	if b.MaxInterval != "" {
		maxMS, err = parseDurationMS("max_interval", b.MaxInterval)
		if err != nil {
			return nil, err
		}
	}
	return &AWSBackoff{BaseDelayMS: base, MaxDelayMS: maxMS}, nil
}

func parseDurationMS(field, value string) (int, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parsing %s %q: %w", field, value, err)
	}
	return int(d.Milliseconds()), nil
}
