package s3

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client wraps AWS S3 client functionality
type Client struct {
	s3Client *s3.Client
	region   string
}

// NewClient creates a new S3 client
func NewClient(ctx context.Context) (*Client, error) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return &Client{
		s3Client: s3.NewFromConfig(cfg),
		region:   region,
	}, nil
}

// GeneratePresignedURL generates a pre-signed URL for downloading an S3 object
func (c *Client) GeneratePresignedURL(ctx context.Context, s3URI string, expirationMinutes int) (string, error) {
	// Parse S3 URI (format: s3://bucket-name/path/to/object)
	bucket, key, err := parseS3URI(s3URI)
	if err != nil {
		return "", err
	}

	// Create presigner
	presigner := s3.NewPresignClient(c.s3Client)

	// Generate presigned URL
	request, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expirationMinutes) * time.Minute
	})

	if err != nil {
		return "", fmt.Errorf("generate presigned URL: %w", err)
	}

	return request.URL, nil
}

// parseS3URI parses an S3 URI into bucket and key components
// Expected format: s3://bucket-name/path/to/object
func parseS3URI(s3URI string) (bucket, key string, err error) {
	if !strings.HasPrefix(s3URI, "s3://") {
		return "", "", fmt.Errorf("invalid S3 URI format: %s (expected s3://bucket/key)", s3URI)
	}

	// Remove s3:// prefix
	path := strings.TrimPrefix(s3URI, "s3://")

	// Split into bucket and key
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid S3 URI format: %s (missing key)", s3URI)
	}

	return parts[0], parts[1], nil
}

// ParseS3URI is a public wrapper for parseS3URI
func ParseS3URI(s3URI string) (bucket, key string, err error) {
	return parseS3URI(s3URI)
}

// GetObjectURL returns a direct URL to an S3 object (without presigning)
// This is useful for public objects or when direct S3 URLs are needed
func (c *Client) GetObjectURL(bucket, key string) string {
	// URL encode the key
	encodedKey := url.PathEscape(key)
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, c.region, encodedKey)
}

// DownloadFileContent downloads file content from S3
func (c *Client) DownloadFileContent(ctx context.Context, bucket, key string) ([]byte, error) {
	result, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("download from S3: %w", err)
	}
	defer result.Body.Close()

	// Read the entire content
	data := make([]byte, 0, *result.ContentLength)
	buf := make([]byte, 4096)
	for {
		n, err := result.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("read S3 object: %w", err)
		}
	}

	return data, nil
}

// DownloadFile is a convenience function that creates a client and downloads a file
func DownloadFile(ctx context.Context, bucket, key string) ([]byte, error) {
	client, err := NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.DownloadFileContent(ctx, bucket, key)
}
