package metrics

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const (
	// Namespace for all OCPCTL metrics
	Namespace = "OCPCTL"

	// Metric names
	MetricPendingJobs      = "PendingJobs"
	MetricRunningJobs      = "RunningJobs"
	MetricJobStarted       = "JobStarted"
	MetricJobSucceeded     = "JobSucceeded"
	MetricJobFailed        = "JobFailed"
	MetricJobRetried       = "JobRetried"
	MetricJobDeferred      = "JobDeferred"
	MetricWorkerActive     = "WorkerActive"
	MetricLockAcquired     = "LockAcquired"
	MetricLockFailed       = "LockFailed"
)

// Publisher publishes metrics to CloudWatch
type Publisher struct {
	client    *cloudwatch.Client
	namespace string
	enabled   bool
}

// NewPublisher creates a new CloudWatch metrics publisher
func NewPublisher(ctx context.Context) (*Publisher, error) {
	// Check if CloudWatch is enabled via environment variable
	// This allows disabling CloudWatch in local development
	enabled := true

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("Warning: Failed to load AWS config for CloudWatch metrics: %v", err)
		// Don't fail - just disable metrics
		return &Publisher{enabled: false}, nil
	}

	client := cloudwatch.NewFromConfig(cfg)

	return &Publisher{
		client:    client,
		namespace: Namespace,
		enabled:   enabled,
	}, nil
}

// PublishGauge publishes a gauge metric (current value at a point in time)
func (p *Publisher) PublishGauge(ctx context.Context, metricName string, value float64, dimensions map[string]string) error {
	if !p.enabled {
		return nil
	}

	metricData := []types.MetricDatum{
		{
			MetricName: aws.String(metricName),
			Value:      aws.Float64(value),
			Timestamp:  aws.Time(time.Now()),
			Unit:       types.StandardUnitCount,
			Dimensions: makeDimensions(dimensions),
		},
	}

	_, err := p.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(p.namespace),
		MetricData: metricData,
	})

	if err != nil {
		return fmt.Errorf("failed to publish gauge metric %s: %w", metricName, err)
	}

	return nil
}

// PublishCount publishes a count metric (event occurrence)
func (p *Publisher) PublishCount(ctx context.Context, metricName string, count float64, dimensions map[string]string) error {
	if !p.enabled {
		return nil
	}

	metricData := []types.MetricDatum{
		{
			MetricName: aws.String(metricName),
			Value:      aws.Float64(count),
			Timestamp:  aws.Time(time.Now()),
			Unit:       types.StandardUnitCount,
			Dimensions: makeDimensions(dimensions),
		},
	}

	_, err := p.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(p.namespace),
		MetricData: metricData,
	})

	if err != nil {
		return fmt.Errorf("failed to publish count metric %s: %w", metricName, err)
	}

	return nil
}

// PublishDuration publishes a duration metric in milliseconds
func (p *Publisher) PublishDuration(ctx context.Context, metricName string, duration time.Duration, dimensions map[string]string) error {
	if !p.enabled {
		return nil
	}

	metricData := []types.MetricDatum{
		{
			MetricName: aws.String(metricName),
			Value:      aws.Float64(float64(duration.Milliseconds())),
			Timestamp:  aws.Time(time.Now()),
			Unit:       types.StandardUnitMilliseconds,
			Dimensions: makeDimensions(dimensions),
		},
	}

	_, err := p.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace:  aws.String(p.namespace),
		MetricData: metricData,
	})

	if err != nil {
		return fmt.Errorf("failed to publish duration metric %s: %w", metricName, err)
	}

	return nil
}

// makeDimensions converts a map of dimensions to CloudWatch dimension format
func makeDimensions(dims map[string]string) []types.Dimension {
	dimensions := make([]types.Dimension, 0, len(dims))
	for key, value := range dims {
		dimensions = append(dimensions, types.Dimension{
			Name:  aws.String(key),
			Value: aws.String(value),
		})
	}
	return dimensions
}
