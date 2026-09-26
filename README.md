# retryconv

Converts a retry policy written for Envoy/Istio into the retry config shape
an AWS SDK client expects.

## why

If you run services behind an Istio sidecar, retries usually live in a
`VirtualService`'s `retries` block, expressed in Envoy's terms: a number of
retries, a per-try timeout, and a `retry_on` condition string like
`5xx,reset,connect-failure`. If part of that traffic moves to something
that talks to AWS directly (a Lambda, a client library that isn't behind the
mesh), the equivalent policy has to be re-expressed as an AWS SDK retry
config: `max_attempts`, a `mode`, and a list of retryable HTTP status codes.

The two formats don't line up cleanly:

- Envoy's `num_retries` excludes the initial attempt; AWS's `max_attempts`
  includes it.
- Envoy conditions like `5xx` or `gateway-error` are wildcards over status
  codes; AWS wants the literal list.
- Envoy conditions like `reset` or `connect-failure` describe something
  that happens *before* there's a status code at all. AWS retry config has
  no way to express that - it only ever reacts to a response it received.
- `per_try_timeout` has no AWS retry-config equivalent either.

Silently dropping any of that is exactly the kind of thing that turns into
an incident three months later when someone assumes retries are happening
that aren't. So by default this tool refuses to guess: anything that can't
be mapped exactly is a hard error. Pass `--lenient` to convert anyway,
dropping what can't be represented and printing a warning for each thing it
drops.

## usage

```
$ cat policy.json
{
  "num_retries": 3,
  "per_try_timeout": "2s",
  "retry_on": "5xx,reset,connect-failure",
  "retry_back_off": {
    "base_interval": "0.025s",
    "max_interval": "0.25s"
  }
}

$ go run . -in policy.json
retryconv: per_try_timeout has no equivalent in the aws retry format; rerun with --lenient to drop it
```

Strict mode stopped here because `per_try_timeout` (and, if it got that
far, the `reset`/`connect-failure` tokens) can't be carried over. Rerunning
with `--lenient`:

```
$ go run . -in policy.json -lenient
retryconv: warning: dropped per_try_timeout "2s": not representable in aws retry config
retryconv: warning: dropped retry_on token(s) with no status-code equivalent: reset, connect-failure
{
  "max_attempts": 4,
  "mode": "standard",
  "backoff": {
    "base_delay_ms": 25,
    "max_delay_ms": 250
  },
  "retryable_status_codes": [
    500,
    502,
    503,
    504
  ]
}
```

Note `max_attempts` is 4, not 3: Envoy's `num_retries` counts retries only,
AWS's `max_attempts` counts the initial try too.

Reads from stdin and writes to stdout by default, so it composes:

```
$ istioctl get virtualservice checkout -o json | jq .spec.http[0].retries | go run . -lenient
```

Use `-in` / `-out` to read or write files instead of stdin/stdout.

## flags

| flag        | default | meaning                                                    |
|-------------|---------|-------------------------------------------------------------|
| `-in`       | `-`     | input file with an Envoy retry policy (JSON); `-` for stdin  |
| `-out`      | `-`     | output file for the AWS retry policy (JSON); `-` for stdout  |
| `-lenient`  | `false` | drop unmappable fields/conditions instead of failing         |

## status

Envoy -> AWS only, for now. See the roadmap for what's next.
