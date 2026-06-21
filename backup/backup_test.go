package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompressFile(t *testing.T) {
	// Create a dummy file to compress
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "dummy.db")
	err := os.WriteFile(srcPath, []byte("dummy database content"), 0644)
	assert.NoError(t, err)

	dstPath := filepath.Join(tmpDir, "dummy_backup.tar.gz")

	// Compress
	err = compressFile(srcPath, dstPath)
	assert.NoError(t, err)

	// Verify compressed file exists
	_, err = os.Stat(dstPath)
	assert.NoError(t, err)

	// Verify decompression
	f, err := os.Open(dstPath)
	assert.NoError(t, err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	assert.NoError(t, err)
	defer gr.Close()

	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	assert.NoError(t, err)
	assert.Equal(t, "dummy.db", hdr.Name)

	content, err := io.ReadAll(tr)
	assert.NoError(t, err)
	assert.Equal(t, "dummy database content", string(content))
}
