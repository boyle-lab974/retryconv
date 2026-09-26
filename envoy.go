package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// EnvoyRetryBackOff mirrors the retry_back_off field of Envoy's RetryPolicy
// proto (config.route.v3.RetryPolicy), JSON-mapped.
type EnvoyRetryBackOff struct {
	BaseInterval string `json:"base_interval"`
	MaxInterval  string `json:"max_interval,omitempty"`
}

// EnvoyRetryPolicy mirrors the subset of Envoy/Istio's RetryPolicy proto
// that this converter understands. Durations are strings such as "2s" or
// "25ms", matching Envoy's own JSON mapping for google.protobuf.Duration.
type EnvoyRetryPolicy struct {
	NumRetries    *int               `json:"num_retries,omitempty"`
	PerTryTimeout string             `json:"per_try_timeout,omitempty"`
	RetryOn       string             `json:"retry_on"`
	RetryBackOff  *EnvoyRetryBackOff `json:"retry_back_off,omitempty"`
}

// ParseEnvoyRetryPolicy reads an Envoy retry policy from JSON. In strict
// mode (lenient=false) unknown fields are rejected outright, on the theory
// that a typo'd field name silently doing nothing is worse than a load
// failure. Lenient mode ignores fields it doesn't recognize.
func ParseEnvoyRetryPolicy(data []byte, lenient bool) (*EnvoyRetryPolicy, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if !lenient {
		dec.DisallowUnknownFields()
	}

	var p EnvoyRetryPolicy
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parsing envoy retry policy: %w", err)
	}
	if p.RetryOn == "" {
		return nil, fmt.Errorf("retry_on is required (Envoy treats a missing value as \"never retry\", which is rarely what a converted policy should mean)")
	}
	return &p, nil
}
