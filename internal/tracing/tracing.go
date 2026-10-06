// Package tracing configures OpenTelemetry to send traces to X-Ray through the AWS Distro for OpenTelemetry (ADOT)
// collector Lambda layer. bot-lambda creates the spans; this package only sets up where they go.
// See https://aws-otel.github.io/docs/getting-started/lambda/lambda-go.
package tracing

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/contrib/detectors/aws/lambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup sets the global tracer provider to export traces to the collector layer, and returns the options for
// otellambda.InstrumentHandler. Only call it when the collector layer is present, otherwise exporting the spans at the
// end of each invocation fails.
//
// Traces are exported over OTLP/HTTP to the collector on localhost:4318, which can be changed with the standard
// OTEL_EXPORTER_OTLP_ENDPOINT variable. The service name comes from OTEL_SERVICE_NAME.
func Setup(ctx context.Context) ([]otellambda.Option, error) {
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	detected, err := lambda.NewResourceDetector().Detect(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect lambda resource: %w", err)
	}

	res, err := resource.Merge(resource.Default(), detected)
	if err != nil {
		return nil, fmt.Errorf("merge resources: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		// X-Ray requires trace IDs which start with the time
		sdktrace.WithIDGenerator(xray.NewIDGenerator()),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(xray.Propagator{})

	return []otellambda.Option{
		otellambda.WithTracerProvider(provider),
		// spans must be exported before the invocation ends, as Lambda may freeze the environment afterwards
		otellambda.WithFlusher(provider),
		otellambda.WithPropagator(xray.Propagator{}),
		otellambda.WithEventToCarrier(xrayEventToCarrier),
	}, nil
}

// xrayEventToCarrier continues the trace Lambda started for the invocation, whose header Lambda sets in the environment
func xrayEventToCarrier([]byte) propagation.TextMapCarrier {
	return propagation.HeaderCarrier{"X-Amzn-Trace-Id": []string{os.Getenv("_X_AMZN_TRACE_ID")}}
}
