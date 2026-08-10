package scanner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mock helper
func helperProcessContext(args ...string) {
	if len(os.Args) > 2 && os.Args[1] == "-test.run=TestHelperProcessContext" {
		if os.Getenv("FAIL_RUN") == "1" {
			fmt.Fprintf(os.Stderr, "failed run")
			os.Exit(1)
		}
		if os.Getenv("BAD_JSON") == "1" {
			fmt.Print("{ bad json")
			os.Exit(0)
		}

		fmt.Print(`{
			"Results": [
				{
					"Vulnerabilities": [
						{
							"VulnerabilityID": "GO-2026-5932",
							"Severity": "CRITICAL"
						},
						{
							"VulnerabilityID": "CVE-2024-1234",
							"Severity": "HIGH"
						}
					]
				}
			]
		}`)
		os.Exit(0)
	}
}

func TestHelperProcessContext(t *testing.T) {
	helperProcessContext()
}

func mockExecCommandContext(ctx context.Context, command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcessContext", "--", command}
	cs = append(cs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	return cmd
}

func TestScanImageFunc_Success(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()
	execCommandContext = mockExecCommandContext

	ctx := context.Background()
	res, err := ScanImageFunc(ctx, nil, "alpine:latest")
	assert.NoError(t, err)

	results := res["Results"].([]interface{})
	target := results[0].(map[string]interface{})
	vulns := target["Vulnerabilities"].([]interface{})

	// Ensure GO-2026-5932 is filtered out, but CVE-2024-1234 is kept
	assert.Equal(t, 1, len(vulns))
	v := vulns[0].(map[string]interface{})
	assert.Equal(t, "CVE-2024-1234", v["VulnerabilityID"])
}

func TestScanImageFunc_RunFail(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()
	execCommandContext = mockExecCommandContext

	t.Setenv("FAIL_RUN", "1")

	ctx := context.Background()
	_, err := ScanImageFunc(ctx, nil, "alpine:latest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trivy scan failed")
}

func TestScanImageFunc_BadJSON(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()
	execCommandContext = mockExecCommandContext

	t.Setenv("BAD_JSON", "1")

	ctx := context.Background()
	_, err := ScanImageFunc(ctx, nil, "alpine:latest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse trivy json output")
}

func TestScanImageFunc_EmptyResult(t *testing.T) {
	origExec := execCommandContext
	defer func() { execCommandContext = origExec }()
	// return empty json
	execCommandContext = func(ctx context.Context, command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcessContextEmpty", "--", command}
		cs = append(cs, args...)
		return exec.CommandContext(ctx, os.Args[0], cs...)
	}

	ctx := context.Background()
	res, err := ScanImageFunc(ctx, nil, "alpine:latest")
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestHelperProcessContextEmpty(t *testing.T) {
	if len(os.Args) > 2 && os.Args[1] == "-test.run=TestHelperProcessContextEmpty" {
		fmt.Print(`{}`)
		os.Exit(0)
	}
}
