package main

// AWSBackoff is the exponential backoff shape used by the AWS SDK "standard"
// and "adaptive" retry modes.
type AWSBackoff struct {
	BaseDelayMS int `json:"base_delay_ms"`
	MaxDelayMS  int `json:"max_delay_ms"`
}

// AWSRetryPolicy is the retry configuration shape accepted by AWS SDK
// clients (see the SDK's RetryMaxAttempts / RetryMode / Backoff options).
// It has no notion of connection-level retry conditions like Envoy's
// "reset" or "connect-failure": it only ever retries based on the HTTP
// status code (or a handful of SDK-internal error codes) it got back.
type AWSRetryPolicy struct {
	MaxAttempts          int         `json:"max_attempts"`
	Mode                 string      `json:"mode"`
	Backoff              *AWSBackoff `json:"backoff,omitempty"`
	RetryableStatusCodes []int       `json:"retryable_status_codes,omitempty"`
}
