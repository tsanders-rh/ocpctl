package middleware

import (
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Logger returns a middleware that logs HTTP requests
func Logger() echo.MiddlewareFunc {
	return middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"time":"${time_rfc3339}","id":"${id}","remote_ip":"${remote_ip}",` +
			`"method":"${method}","uri":"${uri}","status":${status},"error":"${error}",` +
			`"latency":${latency},"latency_human":"${latency_human}",` +
			`"bytes_in":${bytes_in},"bytes_out":${bytes_out}}` + "\n",
		CustomTimeFormat: time.RFC3339,
	})
}
