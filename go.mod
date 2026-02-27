module github.com/tsanders-rh/ocpctl

go 1.22

require (
	// Database
	github.com/jackc/pgx/v5 v5.5.5
	github.com/pressly/goose/v3 v3.18.0

	// HTTP routing and middleware
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1

	// AWS SDK
	github.com/aws/aws-sdk-go-v2 v1.26.0
	github.com/aws/aws-sdk-go-v2/config v1.27.7
	github.com/aws/aws-sdk-go-v2/service/s3 v1.51.4
	github.com/aws/aws-sdk-go-v2/service/sqs v1.31.3
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.28.4

	// Configuration
	github.com/kelseyhightower/envconfig v1.4.0

	// Logging
	golang.org/x/exp v0.0.0-20240222234643-814bf88cf225

	// Validation
	github.com/go-playground/validator/v10 v10.19.0

	// YAML parsing
	gopkg.in/yaml.v3 v3.0.1

	// Testing
	github.com/stretchr/testify v1.9.0
	github.com/testcontainers/testcontainers-go v0.29.1

	// Metrics
	github.com/prometheus/client_golang v1.19.0

	// UUID generation
	github.com/segmentio/ksuid v1.0.4
)
