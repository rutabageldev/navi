// Package events provides CloudEvents v1.0 envelope construction and trace
// context propagation for Navi NATS messages.
package events

import (
	"context"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
)

// Envelope wraps a CloudEvents v1.0 event with Navi extension attributes.
// All events published to NATS must be created via NewEnvelope.
type Envelope struct {
	event cloudevents.Event
}

// NewEnvelope constructs a validated CloudEvents envelope. eventType must be a
// registered type from docs/events/REGISTRY.md. source follows the pattern
// "/navi/{env}/{service}/{component}". schema is the navischema value, e.g.
// "articles.collected/v1". data is JSON-serialisable and populates the data field.
func NewEnvelope(eventType, source, environment, schema string, data interface{}) (*Envelope, error) {
	e := cloudevents.NewEvent()
	e.SetSpecVersion("1.0")
	e.SetID(uuid.New().String())
	e.SetSource(source)
	e.SetType(eventType)
	e.SetTime(time.Now().UTC())
	e.SetDataContentType(cloudevents.ApplicationJSON)
	e.SetExtension("navienv", environment)
	e.SetExtension("navischema", schema)
	e.SetExtension("traceparent", "")
	e.SetExtension("tracestate", "")

	if err := e.SetData(cloudevents.ApplicationJSON, data); err != nil {
		return nil, fmt.Errorf("setting event data: %w", err)
	}

	return &Envelope{event: e}, nil
}

// InjectTrace copies W3C traceparent/tracestate from the active span in ctx
// into the envelope's extension attributes.
func InjectTrace(ctx context.Context, env *Envelope) {
	otel.GetTextMapPropagator().Inject(ctx, envelopeCarrier{e: &env.event})
}

// ExtractTrace extracts W3C traceparent/tracestate from the envelope's extension
// attributes and returns a context carrying the remote span.
func ExtractTrace(env *Envelope) context.Context {
	return otel.GetTextMapPropagator().Extract(context.Background(), envelopeCarrier{e: &env.event})
}

// envelopeCarrier adapts an Envelope to the propagation.TextMapCarrier interface
// so that the OTEL propagator can read and write extension attributes.
type envelopeCarrier struct {
	e *cloudevents.Event
}

func (c envelopeCarrier) Get(key string) string {
	ext := c.e.Extensions()
	if ext == nil {
		return ""
	}
	v, ok := ext[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (c envelopeCarrier) Set(key, value string) {
	c.e.SetExtension(key, value)
}

func (c envelopeCarrier) Keys() []string {
	ext := c.e.Extensions()
	keys := make([]string, 0, len(ext))
	for k := range ext {
		keys = append(keys, k)
	}
	return keys
}
