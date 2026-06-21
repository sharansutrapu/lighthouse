package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanImage_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ScanImage(ctx, nil, "alpine:latest")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trivy scan failed")
}
