package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSetup(t *testing.T) {
	t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "pinbot-test")
	t.Setenv("AWS_REGION", "eu-west-1")

	provider, propagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
	})

	opts, err := Setup(context.Background())

	require.NoError(t, err)
	assert.Len(t, opts, 4)
	assert.IsType(t, &sdktrace.TracerProvider{}, otel.GetTracerProvider())
}

func TestXrayEventToCarrier(t *testing.T) {
	t.Setenv("_X_AMZN_TRACE_ID", "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1")

	carrier := xrayEventToCarrier(nil)

	assert.Equal(t, "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1", carrier.Get("X-Amzn-Trace-Id"))
	assert.Implements(t, (*propagation.TextMapCarrier)(nil), carrier)
}
