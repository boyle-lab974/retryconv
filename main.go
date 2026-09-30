// Command retryconv converts a retry policy from Envoy/Istio's format to
// the retry config shape used by AWS SDK clients.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	inPath := flag.String("in", "-", "input file with an Envoy retry policy in JSON (- for stdin)")
	outPath := flag.String("out", "-", "output file for the converted AWS retry policy in JSON (- for stdout)")
	lenient := flag.Bool("lenient", false, "allow lossy or ambiguous conversions (drop what can't be mapped) instead of failing")
	reverse := flag.Bool("reverse", false, "convert an AWS retry policy to an Envoy one instead")
	flag.Parse()

	if err := run(*inPath, *outPath, *lenient, *reverse); err != nil {
		fmt.Fprintln(os.Stderr, "retryconv:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath string, lenient, reverse bool) error {
	data, err := readInput(inPath)
	if err != nil {
		return err
	}

	var result any
	var warnings []string
	if reverse {
		awsPolicy, perr := ParseAWSRetryPolicy(data, lenient)
		if perr != nil {
			return perr
		}
		result, warnings, err = AWSToEnvoy(awsPolicy, lenient)
	} else {
		envoyPolicy, perr := ParseEnvoyRetryPolicy(data, lenient)
		if perr != nil {
			return perr
		}
		result, warnings, err = EnvoyToAWS(envoyPolicy, lenient)
	}
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "retryconv: warning:", w)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	return writeOutput(outPath, out)
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeOutput(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
