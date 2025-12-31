package storage

import (
	"context"
	// "fmt"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOClient struct {
	client *minio.Client
	bucketName string
}

func NewMinIOClient(endPoint, accessKey, secretKey, bucketName string)(*MinIOClient, error){
	client, err := minio.New(endPoint, &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false, // Use true for HTTPS
	})
	if err != nil {
		return nil, err
	}

	// Create bucket if it doesn't exist
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, err
	}

	if !exists {
		err = client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
	}

	return &MinIOClient{
		client: client,
		bucketName: bucketName,
	}, nil
}

// Generate presigned URL for uploading
func (m *MinIOClient) GetPresignedPutURL(ctx context.Context, objectKey string, expiry time.Duration)(string, error){
	url, err := m.client.PresignedPutObject(ctx, m.bucketName, objectKey, expiry)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// Generate presigned URL for downloading
func (m *MinIOClient) GetPresignedGetURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	url, err := m.client.PresignedGetObject(ctx, m.bucketName, objectKey, expiry, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// Delete object
func (m *MinIOClient) DeleteObject(ctx context.Context, objectKey string) error {
	return m.client.RemoveObject(ctx, m.bucketName, objectKey, minio.RemoveObjectOptions{})
}