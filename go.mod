module github.com/tsanders-rh/ocpctl

go 1.25.0

require (
	// Validation
	github.com/go-playground/validator/v10 v10.28.0
	// Database
	github.com/jackc/pgx/v5 v5.5.5

	// UUID generation
	github.com/segmentio/ksuid v1.0.4

	// Testing
	github.com/stretchr/testify v1.11.1

	// YAML parsing
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/aws/aws-sdk-go-v2 v1.41.3
	github.com/aws/aws-sdk-go-v2/config v1.32.10
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.293.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.54.7
	github.com/aws/aws-sdk-go-v2/service/iam v1.53.4
	github.com/aws/aws-sdk-go-v2/service/route53 v1.62.2
	github.com/aws/aws-sdk-go-v2/service/s3 v1.96.2
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.7
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/labstack/echo/v4 v4.15.1
	golang.org/x/crypto v0.49.0
	golang.org/x/time v0.14.0
)

require (
	github.com/IBM/go-sdk-core/v5 v5.21.2 // indirect
	github.com/IBM/platform-services-go-sdk v0.96.0 // indirect
	github.com/IBM/vpc-go-sdk v0.80.0 // indirect
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.5 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.19 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.19 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.55.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.15 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/go-openapi/errors v0.22.4 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.5 // indirect
	github.com/go-openapi/spec v0.22.4 // indirect
	github.com/go-openapi/strfmt v0.25.0 // indirect
	github.com/go-openapi/swag v0.25.5 // indirect
	github.com/go-openapi/swag/conv v0.25.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.5 // indirect
	github.com/go-openapi/swag/loading v0.25.5 // indirect
	github.com/go-openapi/swag/stringutils v0.25.5 // indirect
	github.com/go-openapi/swag/typeutils v0.25.5 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.5 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mailru/easyjson v0.9.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/sv-tools/openapi v0.4.0 // indirect
	github.com/swaggo/echo-swagger v1.5.2 // indirect
	github.com/swaggo/files v1.0.1 // indirect
	github.com/swaggo/files/v2 v2.0.2 // indirect
	github.com/swaggo/swag v1.16.6 // indirect
	github.com/swaggo/swag/v2 v2.0.0-rc5 // indirect
	github.com/urfave/cli/v2 v2.27.7 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/xrash/smetrics v0.0.0-20250705151800-55b8f293f342 // indirect
	go.mongodb.org/mongo-driver v1.17.6 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
