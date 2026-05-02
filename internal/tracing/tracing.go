package tracing

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// ServiceNameAPI is the service name for the API server
	ServiceNameAPI = "ocpctl-api"
	// ServiceNameWorker is the service name for the worker
	ServiceNameWorker = "ocpctl-worker"
)

var (
	// Tracer is the global tracer instance
	Tracer trace.Tracer
)

// Config holds tracing configuration
type Config struct {
	// Enabled enables OpenTelemetry tracing
	Enabled bool
	// ServiceName is the name of the service (api or worker)
	ServiceName string
	// Environment is the deployment environment (dev, staging, prod)
	Environment string
	// OTLPEndpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	// If empty, traces are exported to stdout
	OTLPEndpoint string
	// SamplingRate is the sampling rate (0.0 to 1.0)
	// 1.0 = trace all requests, 0.1 = trace 10% of requests
	SamplingRate float64
}

// DefaultConfig returns default tracing configuration from environment variables
func DefaultConfig(serviceName string) *Config {
	enabled := os.Getenv("OTEL_ENABLED") == "true"
	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "development"
	}

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	samplingRate := 1.0 // Default: trace all requests
	if environment == "production" {
		samplingRate = 0.1 // Production: sample 10% to reduce overhead
	}

	return &Config{
		Enabled:      enabled,
		ServiceName:  serviceName,
		Environment:  environment,
		OTLPEndpoint: otlpEndpoint,
		SamplingRate: samplingRate,
	}
}

// InitTracer initializes the OpenTelemetry tracer provider
func InitTracer(ctx context.Context, cfg *Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		log.Printf("OpenTelemetry tracing is disabled")
		// Return a no-op shutdown function
		return func(context.Context) error { return nil }, nil
	}

	log.Printf("Initializing OpenTelemetry tracing (service=%s, environment=%s, sampling=%.2f)",
		cfg.ServiceName, cfg.Environment, cfg.SamplingRate)

	// Create resource with service metadata
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
			semconv.DeploymentEnvironment(cfg.Environment),
			attribute.String("service.namespace", "ocpctl"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// Create trace exporter
	var exporter sdktrace.SpanExporter
	if cfg.OTLPEndpoint != "" {
		// Export to OTLP collector (Jaeger, AWS X-Ray, etc.)
		log.Printf("Using OTLP exporter (endpoint=%s)", cfg.OTLPEndpoint)
		otlpExporter, err := otlptrace.New(ctx,
			otlptracegrpc.NewClient(
				otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
				otlptracegrpc.WithInsecure(), // Use TLS in production
			),
		)
		if err != nil {
			return nil, fmt.Errorf("create OTLP exporter: %w", err)
		}
		exporter = otlpExporter
	} else {
		// Export to stdout for local development
		log.Printf("Using stdout exporter for local development")
		stdoutExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, fmt.Errorf("create stdout exporter: %w", err)
		}
		exporter = stdoutExporter
	}

	// Create trace provider with sampling
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SamplingRate)),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagators for context propagation across services
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create global tracer
	Tracer = tp.Tracer(cfg.ServiceName)

	log.Printf("OpenTelemetry tracing initialized successfully")

	// Return shutdown function
	return tp.Shutdown, nil
}

// StartSpan starts a new span with the given name and options
func StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if Tracer == nil {
		// Tracing not initialized, return no-op span
		return ctx, trace.SpanFromContext(ctx)
	}
	return Tracer.Start(ctx, spanName, opts...)
}

// AddSpanAttributes adds attributes to the current span
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// AddSpanEvent adds an event to the current span
func AddSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// RecordError records an error on the current span
func RecordError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err, trace.WithAttributes(attrs...))
	}
}
