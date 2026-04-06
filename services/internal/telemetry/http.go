package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPHandler wraps an http.Handler with OpenTelemetry instrumentation,
// creating a span for each request named after operation.
func NewHTTPHandler(operation string, handler http.Handler) http.Handler {
	return otelhttp.NewHandler(handler, operation)
}
