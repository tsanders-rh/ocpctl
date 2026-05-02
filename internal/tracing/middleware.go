package tracing

import (
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// Middleware returns an Echo middleware that traces HTTP requests
func Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip tracing if tracer is not initialized
			if Tracer == nil {
				return next(c)
			}

			req := c.Request()
			ctx := req.Context()

			// Start span for this HTTP request
			spanName := req.Method + " " + c.Path()
			ctx, span := Tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPMethod(req.Method),
					semconv.HTTPTarget(req.URL.Path),
					semconv.HTTPScheme(req.URL.Scheme),
					semconv.HTTPRoute(c.Path()),
					semconv.HTTPURL(req.URL.String()),
					semconv.HTTPRequestContentLength(int(req.ContentLength)),
					semconv.UserAgentOriginal(req.UserAgent()),
					attribute.String("http.client_ip", c.RealIP()),
				),
			)
			defer span.End()

			// Update request context with span
			c.SetRequest(req.WithContext(ctx))

			// Call next handler
			err := next(c)

			// Record response status
			status := c.Response().Status
			span.SetAttributes(
				semconv.HTTPStatusCode(status),
				semconv.HTTPResponseContentLength(int(c.Response().Size)),
			)

			// Mark span as error if status >= 400
			if status >= 400 {
				span.SetStatus(codes.Error, "HTTP error")
			} else {
				span.SetStatus(codes.Ok, "")
			}

			// Record error if present
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}

			return err
		}
	}
}
