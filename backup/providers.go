package backup

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"cloud.google.com/go/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/api/option"
)

// StorageProvider is the interface for uploading backups
type StorageProvider interface {
	Upload(ctx context.Context, bucket, objectName, filePath string) error
}

// S3Provider implements StorageProvider for AWS S3 and MinIO
type S3Provider struct {
	client *minio.Client
}

func NewS3Provider(endpoint, accessKey, secretKey, region string) (*S3Provider, error) {
	// If endpoint has https://, we must pass secure=true, else false
	secure := true
	if len(endpoint) > 7 && endpoint[:7] == "http://" {
		secure = false
		endpoint = endpoint[7:]
	} else if len(endpoint) > 8 && endpoint[:8] == "https://" {
		endpoint = endpoint[8:]
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	return &S3Provider{client: client}, nil
}

func (s *S3Provider) Upload(ctx context.Context, bucket, objectName, filePath string) error {
	_, err := s.client.FPutObject(ctx, bucket, objectName, filePath, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	return err
}

// GCSProvider implements StorageProvider for Google Cloud Storage
type GCSProvider struct {
	client *storage.Client
}

func NewGCSProvider(ctx context.Context, jsonKey string) (*GCSProvider, error) {
	client, err := storage.NewClient(ctx, option.WithCredentialsJSON([]byte(jsonKey)))
	if err != nil {
		return nil, err
	}
	return &GCSProvider{client: client}, nil
}

func (g *GCSProvider) Upload(ctx context.Context, bucket, objectName, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	wc := g.client.Bucket(bucket).Object(objectName).NewWriter(ctx)
	wc.ContentType = "application/gzip"
	if _, err := io.Copy(wc, f); err != nil {
		wc.Close()
		return err
	}
	return wc.Close()
}

// AzureProvider implements StorageProvider for Azure Blob Storage
type AzureProvider struct {
	client *azblob.Client
}

func NewAzureProvider(accountName, accountKey string) (*AzureProvider, error) {
	cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, err
	}
	
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, err
	}
	
	return &AzureProvider{client: client}, nil
}

func (a *AzureProvider) Upload(ctx context.Context, containerName, objectName, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = a.client.UploadFile(ctx, containerName, objectName, f, nil)
	return err
}
