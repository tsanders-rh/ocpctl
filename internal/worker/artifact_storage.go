package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ArtifactStorage handles storing and retrieving cluster artifacts from S3
type ArtifactStorage struct {
	s3Client   *s3.Client
	bucketName string
}

// NewArtifactStorage creates a new artifact storage instance
func NewArtifactStorage(ctx context.Context, bucketName string) (*ArtifactStorage, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return &ArtifactStorage{
		s3Client:   s3.NewFromConfig(cfg),
		bucketName: bucketName,
	}, nil
}

// UploadClusterArtifacts uploads all cluster artifacts to S3
func (a *ArtifactStorage) UploadClusterArtifacts(ctx context.Context, workDir, clusterID string) error {
	log.Printf("[ArtifactStorage] Uploading artifacts for cluster %s from %s", clusterID, workDir)

	// List of artifacts to upload with their S3 keys
	artifacts := []struct {
		LocalPath string
		S3Key     string
		Required  bool
	}{
		// Auth artifacts
		{
			LocalPath: filepath.Join(workDir, "auth", "kubeconfig"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/auth/kubeconfig", clusterID),
			Required:  true,
		},
		{
			LocalPath: filepath.Join(workDir, "auth", "kubeadmin-password"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/auth/kubeadmin-password", clusterID),
			Required:  true,
		},

		// Metadata
		{
			LocalPath: filepath.Join(workDir, "metadata.json"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/metadata.json", clusterID),
			Required:  true,
		},

		// OpenShift install state (needed for destroy/resume)
		{
			LocalPath: filepath.Join(workDir, ".openshift_install_state.json"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/openshift_install_state.json", clusterID),
			Required:  true,
		},

		// Install config backup
		{
			LocalPath: filepath.Join(workDir, "install-config.yaml.bak"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/install-config.yaml", clusterID),
			Required:  false,
		},

		// Logs
		{
			LocalPath: filepath.Join(workDir, ".openshift_install.log"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/openshift_install.log", clusterID),
			Required:  false,
		},

		// Terraform state (if present)
		{
			LocalPath: filepath.Join(workDir, "terraform.tfstate"),
			S3Key:     fmt.Sprintf("clusters/%s/artifacts/terraform.tfstate", clusterID),
			Required:  false,
		},
	}

	// Upload each artifact
	uploadedCount := 0
	for _, artifact := range artifacts {
		if err := a.uploadFile(ctx, artifact.LocalPath, artifact.S3Key); err != nil {
			if artifact.Required {
				return fmt.Errorf("upload required artifact %s: %w", artifact.LocalPath, err)
			}
			log.Printf("[ArtifactStorage] Warning: failed to upload optional artifact %s: %v", artifact.LocalPath, err)
		} else {
			uploadedCount++
		}
	}

	// Upload entire manifests directory (needed for destroy)
	manifestsDir := filepath.Join(workDir, "manifests")
	if stat, err := os.Stat(manifestsDir); err == nil && stat.IsDir() {
		if err := a.uploadDirectory(ctx, manifestsDir, fmt.Sprintf("clusters/%s/artifacts/manifests", clusterID)); err != nil {
			log.Printf("[ArtifactStorage] Warning: failed to upload manifests directory: %v", err)
		} else {
			log.Printf("[ArtifactStorage] Uploaded manifests directory")
		}
	}

	// Upload tls directory (needed for STS/Manual mode clusters)
	tlsDir := filepath.Join(workDir, "tls")
	if stat, err := os.Stat(tlsDir); err == nil && stat.IsDir() {
		if err := a.uploadDirectory(ctx, tlsDir, fmt.Sprintf("clusters/%s/artifacts/tls", clusterID)); err != nil {
			log.Printf("[ArtifactStorage] Warning: failed to upload tls directory: %v", err)
		} else {
			log.Printf("[ArtifactStorage] Uploaded tls directory")
		}
	}

	log.Printf("[ArtifactStorage] Successfully uploaded %d artifacts for cluster %s", uploadedCount, clusterID)
	return nil
}

// DownloadClusterArtifacts downloads all cluster artifacts from S3 to local work directory
func (a *ArtifactStorage) DownloadClusterArtifacts(ctx context.Context, clusterID, workDir string) error {
	log.Printf("[ArtifactStorage] Downloading artifacts for cluster %s to %s", clusterID, workDir)

	// Ensure work directory exists
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}

	// List all artifacts for this cluster
	prefix := fmt.Sprintf("clusters/%s/artifacts/", clusterID)
	listOutput, err := a.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(a.bucketName),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	if len(listOutput.Contents) == 0 {
		return fmt.Errorf("no artifacts found for cluster %s", clusterID)
	}

	// Download each artifact
	downloadedCount := 0
	for _, obj := range listOutput.Contents {
		s3Key := aws.ToString(obj.Key)

		// Extract relative path from S3 key
		// Example: clusters/<id>/artifacts/auth/kubeconfig -> auth/kubeconfig
		relativePath := strings.TrimPrefix(s3Key, prefix)
		if relativePath == "" {
			continue // Skip directory markers
		}

		localPath := filepath.Join(workDir, relativePath)

		if err := a.downloadFile(ctx, s3Key, localPath); err != nil {
			return fmt.Errorf("download artifact %s: %w", s3Key, err)
		}
		downloadedCount++
	}

	log.Printf("[ArtifactStorage] Successfully downloaded %d artifacts for cluster %s", downloadedCount, clusterID)
	return nil
}

// DeleteClusterArtifacts deletes all cluster artifacts from S3
func (a *ArtifactStorage) DeleteClusterArtifacts(ctx context.Context, clusterID string) error {
	log.Printf("[ArtifactStorage] Deleting artifacts for cluster %s", clusterID)

	prefix := fmt.Sprintf("clusters/%s/", clusterID)

	// List all objects with this prefix
	listOutput, err := a.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(a.bucketName),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	if len(listOutput.Contents) == 0 {
		log.Printf("[ArtifactStorage] No artifacts found for cluster %s", clusterID)
		return nil
	}

	// Delete each object
	deletedCount := 0
	for _, obj := range listOutput.Contents {
		_, err := a.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(a.bucketName),
			Key:    obj.Key,
		})
		if err != nil {
			log.Printf("[ArtifactStorage] Warning: failed to delete %s: %v", *obj.Key, err)
		} else {
			deletedCount++
		}
	}

	log.Printf("[ArtifactStorage] Deleted %d artifacts for cluster %s", deletedCount, clusterID)
	return nil
}

// uploadFile uploads a single file to S3
func (a *ArtifactStorage) uploadFile(ctx context.Context, localPath, s3Key string) error {
	// Open local file
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Get file info for size
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	log.Printf("[ArtifactStorage] Uploading %s (%d bytes) to s3://%s/%s", localPath, stat.Size(), a.bucketName, s3Key)

	// Upload to S3
	_, err = a.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(a.bucketName),
		Key:           aws.String(s3Key),
		Body:          file,
		ContentLength: aws.Int64(stat.Size()),
		ServerSideEncryption: "AES256", // Encrypt artifacts at rest
	})
	if err != nil {
		return fmt.Errorf("upload to S3: %w", err)
	}

	return nil
}

// downloadFile downloads a single file from S3
func (a *ArtifactStorage) downloadFile(ctx context.Context, s3Key, localPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	// Download from S3
	output, err := a.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.bucketName),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return fmt.Errorf("download from S3: %w", err)
	}
	defer output.Body.Close()

	// Create local file
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer file.Close()

	// Copy content
	written, err := io.Copy(file, output.Body)
	if err != nil {
		return fmt.Errorf("write to file: %w", err)
	}

	log.Printf("[ArtifactStorage] Downloaded s3://%s/%s to %s (%d bytes)", a.bucketName, s3Key, localPath, written)
	return nil
}

// uploadDirectory recursively uploads a directory to S3
func (a *ArtifactStorage) uploadDirectory(ctx context.Context, localDir, s3Prefix string) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Calculate relative path
		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return fmt.Errorf("calculate relative path: %w", err)
		}

		// Convert to S3 key (use forward slashes)
		s3Key := fmt.Sprintf("%s/%s", s3Prefix, filepath.ToSlash(relPath))

		// Upload file
		return a.uploadFile(ctx, path, s3Key)
	})
}
