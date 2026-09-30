package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseAWSRetryPolicy reads an AWS retry policy from JSON. As with the Envoy
// parser, strict mode rejects unknown fields so a typo doesn't silently turn
// into a default.
func ParseAWSRetryPolicy(data []byte, lenient bool) (*AWSRetryPolicy, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if !lenient {
		dec.DisallowUnknownFields()
	}

	var p AWSRetryPolicy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parsing aws retry policy: %w", err)
	}
	return &p, nil
}

// AWSToEnvoy converts an AWS SDK retry config to an Envoy/Istio retry policy.
// Like EnvoyToAWS, anything that can't be carried over is an error unless
// lenient is set, in which case it becomes a warning.
func AWSToEnvoy(p *AWSRetryPolicy, lenient bool) (*EnvoyRetryPolicy, []string, error) {
	var warnings []string

	attempts := p.MaxAttempts
	if attempts < 1 {
		if !lenient {
			return nil, nil, fmt.Errorf("max_attempts is %d; it counts the initial try so must be at least 1 (rerun with --lenient to clamp it to 1)", attempts)
		}
		warnings = append(warnings, fmt.Sprintf("max_attempts %d clamped to 1", attempts))
		attempts = 1
	}
	retries := attempts - 1

	switch p.Mode {
	case "", "standard":
	case "adaptive":
		// Adaptive mode adds client-side rate limiting driven by observed
		// throttling; Envoy's retry budget is a different mechanism.
		if !lenient {
			return nil, nil, fmt.Errorf("mode \"adaptive\" has no equivalent in envoy retry policy; rerun with --lenient to treat it as standard")
		}
		warnings = append(warnings, "mode \"adaptive\" treated as standard: client-side rate limiting is not representable in envoy")
	default:
		if !lenient {
			return nil, nil, fmt.Errorf("unrecognized mode %q; rerun with --lenient to treat it as standard", p.Mode)
		}
		warnings = append(warnings, fmt.Sprintf("unrecognized mode %q treated as standard", p.Mode))
	}

	backoff, err := awsBackoffToEnvoy(p.Backoff)
	if err != nil {
		return nil, nil, err
	}

	codes, err := normalizeStatusCodes(p.RetryableStatusCodes)
	if err != nil {
		return nil, nil, err
	}

	out := &EnvoyRetryPolicy{
		NumRetries:   &retries,
		RetryBackOff: backoff,
	}
	if len(codes) == 0 {
		// An empty list in AWS means "the SDK's built-in retryable set",
		// which includes throttling and transient network errors. Envoy has
		// no way to say that, and an empty retry_on would mean never retry.
		if !lenient {
			return nil, nil, fmt.Errorf("retryable_status_codes is empty, which means the sdk default set in aws and has no envoy equivalent; rerun with --lenient to use \"5xx\"")
		}
		warnings = append(warnings, "retryable_status_codes is empty: substituted retry_on \"5xx\" for the sdk default set")
		out.RetryOn = "5xx"
	} else {
		// Envoy's "5xx" and "gateway-error" tokens cover whole ranges, so
		// using them would retry on codes the AWS config never listed.
		// retriable-status-codes keeps the list exact.
		out.RetryOn = "retriable-status-codes"
		out.RetriableStatusCodes = codes
	}
	return out, warnings, nil
}

func awsBackoffToEnvoy(b *AWSBackoff) (*EnvoyRetryBackOff, error) {
	if b == nil {
		return nil, nil
	}
	if b.BaseDelayMS <= 0 {
		return nil, fmt.Errorf("backoff.base_delay_ms must be positive, got %d (envoy rejects a zero base_interval)", b.BaseDelayMS)
	}
	if b.MaxDelayMS < b.BaseDelayMS {
		return nil, fmt.Errorf("backoff.max_delay_ms (%d) is smaller than base_delay_ms (%d)", b.MaxDelayMS, b.BaseDelayMS)
	}
	return &EnvoyRetryBackOff{
		BaseInterval: formatProtoDuration(b.BaseDelayMS),
		MaxInterval:  formatProtoDuration(b.MaxDelayMS),
	}, nil
}

func normalizeStatusCodes(in []int) ([]int, error) {
	seen := map[int]bool{}
	var codes []int
	for _, c := range in {
		if c < 100 || c > 599 {
			return nil, fmt.Errorf("retryable_status_codes contains %d, which is not an http status code", c)
		}
		if !seen[c] {
			seen[c] = true
			codes = append(codes, c)
		}
	}
	sort.Ints(codes)
	return codes, nil
}

// formatProtoDuration renders milliseconds the way protobuf's JSON mapping
// writes a Duration: seconds with an optional fractional part and an "s"
// suffix. Go's own Duration.String would give "1m0s", which Envoy won't parse.
func formatProtoDuration(ms int) string {
	secs := ms / 1000
	rem := ms % 1000
	if rem == 0 {
		return strconv.Itoa(secs) + "s"
	}
	frac := strings.TrimRight(fmt.Sprintf("%03d", rem), "0")
	return strconv.Itoa(secs) + "." + frac + "s"
}
