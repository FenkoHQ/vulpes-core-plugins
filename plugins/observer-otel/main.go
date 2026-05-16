package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	oteltrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	logscollectorv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

type plugin struct {
	mu sync.Mutex

	serviceName string
	endpoint    string
	insecure    bool
	headers     map[string]string
	attrs       []attribute.KeyValue
	logURL      string
	http        *http.Client

	tracerProvider *oteltrace.TracerProvider
	meterProvider  *otelmetric.MeterProvider
	tracer         trace.Tracer
	eventCounter   metric.Int64Counter
	requestCounter metric.Int64Counter
	inputTokens    metric.Int64Counter
	outputTokens   metric.Int64Counter
	totalTokens    metric.Int64Counter
	costUSD        metric.Float64Counter
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.serviceName = firstString(cfg["service_name"], "vulpes-gateway")
	p.endpoint = firstString(cfg["endpoint"], "localhost:4318")
	p.insecure = true
	if v, ok := cfg["insecure"].(bool); ok {
		p.insecure = v
	}
	p.headers = stringMap(cfg["headers"])
	p.attrs = []attribute.KeyValue{semconv.ServiceName(p.serviceName)}
	for k, v := range stringMap(cfg["resource_attributes"]) {
		p.attrs = append(p.attrs, attribute.String(k, v))
	}

	if strings.Contains(p.endpoint, "/insert/opentelemetry") {
		p.logURL = normalizeOTLPLogURL(p.endpoint)
		p.http = &http.Client{Timeout: 10 * time.Second}
		return nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(p.attrs...))
	if err != nil {
		return err
	}

	traceOpts := []otlptracehttp.Option{otlptracehttp.WithHeaders(p.headers)}
	metricOpts := []otlpmetrichttp.Option{otlpmetrichttp.WithHeaders(p.headers)}
	if isEndpointURL(p.endpoint) {
		traceOpts = append(traceOpts, otlptracehttp.WithEndpointURL(p.endpoint))
		metricOpts = append(metricOpts, otlpmetrichttp.WithEndpointURL(p.endpoint))
	} else {
		traceOpts = append(traceOpts, otlptracehttp.WithEndpoint(strings.TrimPrefix(strings.TrimPrefix(p.endpoint, "http://"), "https://")))
		metricOpts = append(metricOpts, otlpmetrichttp.WithEndpoint(strings.TrimPrefix(strings.TrimPrefix(p.endpoint, "http://"), "https://")))
		if p.insecure || strings.HasPrefix(p.endpoint, "http://") {
			traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
			metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		} else {
			traceOpts = append(traceOpts, otlptracehttp.WithTLSClientConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
			metricOpts = append(metricOpts, otlpmetrichttp.WithTLSClientConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
		}
	}

	traceExporter, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return fmt.Errorf("create otlp trace exporter: %w", err)
	}
	metricExporter, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return fmt.Errorf("create otlp metric exporter: %w", err)
	}

	p.tracerProvider = oteltrace.NewTracerProvider(oteltrace.WithBatcher(traceExporter), oteltrace.WithResource(res))
	reader := otelmetric.NewPeriodicReader(metricExporter, otelmetric.WithInterval(5*time.Second))
	p.meterProvider = otelmetric.NewMeterProvider(otelmetric.WithReader(reader), otelmetric.WithResource(res))
	p.tracer = p.tracerProvider.Tracer("vulpes-core/observer-otel")
	meter := p.meterProvider.Meter("vulpes-core/observer-otel")
	p.eventCounter, _ = meter.Int64Counter("vulpes.gateway.events", metric.WithDescription("Gateway lifecycle events"))
	p.requestCounter, _ = meter.Int64Counter("vulpes.gateway.requests", metric.WithDescription("Gateway request results"))
	p.inputTokens, _ = meter.Int64Counter("vulpes.gateway.usage.input_tokens", metric.WithDescription("Input tokens"))
	p.outputTokens, _ = meter.Int64Counter("vulpes.gateway.usage.output_tokens", metric.WithDescription("Output tokens"))
	p.totalTokens, _ = meter.Int64Counter("vulpes.gateway.usage.total_tokens", metric.WithDescription("Total tokens"))
	p.costUSD, _ = meter.Float64Counter("vulpes.gateway.usage.cost_usd", metric.WithDescription("Cost in USD"))
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	p.mu.Lock()
	tracer := p.tracer
	eventCounter := p.eventCounter
	requestCounter := p.requestCounter
	inputTokens := p.inputTokens
	outputTokens := p.outputTokens
	totalTokens := p.totalTokens
	costUSD := p.costUSD
	logURL := p.logURL
	httpClient := p.http
	headers := p.headers
	serviceName := p.serviceName
	p.mu.Unlock()

	if logURL != "" {
		return emitOTLPLogs(ctx, httpClient, logURL, headers, serviceName, events)
	}
	if tracer == nil {
		return nil
	}
	for _, ev := range events {
		attrs := eventAttrs(ev)
		spanCtx, span := tracer.Start(ctx, "gateway."+ev.EventType)
		span.SetAttributes(attrs...)
		if len(ev.Error) > 0 {
			span.SetStatus(codes.Error, ev.Error["message"])
			span.RecordError(fmt.Errorf("%s", ev.Error["message"]))
		}
		span.End(trace.WithTimestamp(time.Unix(0, ev.TimestampUnixNano)))
		eventCounter.Add(spanCtx, 1, metric.WithAttributes(attrs...))
		if ev.EventType == "request.completed" {
			requestCounter.Add(spanCtx, 1, metric.WithAttributes(attribute.String("status", "completed"), attribute.String("tenant_id", ev.TenantID)))
		}
		if ev.EventType == "request.failed" {
			requestCounter.Add(spanCtx, 1, metric.WithAttributes(attribute.String("status", "failed"), attribute.String("tenant_id", ev.TenantID)))
		}
		usageAttrs := []attribute.KeyValue{attribute.String("tenant_id", ev.TenantID), attribute.String("provider", ev.Usage.ProviderInstance), attribute.String("model", ev.Usage.ProviderModel)}
		if ev.Usage.InputTokens > 0 {
			inputTokens.Add(spanCtx, ev.Usage.InputTokens, metric.WithAttributes(usageAttrs...))
		}
		if ev.Usage.OutputTokens > 0 {
			outputTokens.Add(spanCtx, ev.Usage.OutputTokens, metric.WithAttributes(usageAttrs...))
		}
		if ev.Usage.TotalTokens > 0 {
			totalTokens.Add(spanCtx, ev.Usage.TotalTokens, metric.WithAttributes(usageAttrs...))
		}
		if ev.Usage.CostUSD > 0 {
			costUSD.Add(spanCtx, ev.Usage.CostUSD, metric.WithAttributes(usageAttrs...))
		}
	}
	return nil
}

func eventAttrs(ev sdk.GatewayEvent) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("event.type", ev.EventType),
		attribute.String("request.id", ev.RequestID),
		attribute.String("tenant.id", ev.TenantID),
	}
	for k, v := range ev.Properties {
		if k == "request_json" || k == "response_json" {
			continue
		}
		attrs = append(attrs, attribute.String("gateway."+sanitizeAttr(k), v))
	}
	for k, v := range ev.Error {
		attrs = append(attrs, attribute.String("error."+sanitizeAttr(k), v))
	}
	return attrs
}

func sanitizeAttr(s string) string {
	return strings.NewReplacer(" ", "_", "-", "_", "/", "_").Replace(s)
}

func isEndpointURL(s string) bool {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return false
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "http://"), "https://")
	return strings.Contains(trimmed, "/")
}

func normalizeOTLPLogURL(endpoint string) string {
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.HasSuffix(endpoint, "/v1/logs") {
		return endpoint
	}
	return endpoint + "/v1/logs"
}

func emitOTLPLogs(ctx context.Context, client *http.Client, url string, headers map[string]string, serviceName string, events []sdk.GatewayEvent) error {
	if client == nil {
		client = http.DefaultClient
	}
	records := make([]*logsv1.LogRecord, 0, len(events))
	for _, ev := range events {
		records = append(records, eventLogRecord(ev))
	}
	req := &logscollectorv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		Resource:  &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{Key: "service.name", Value: stringValue(serviceName)}}},
		ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: records}},
	}}}
	body, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("otlp logs export to %s: %s", url, resp.Status)
	}
	return nil
}

func eventLogRecord(ev sdk.GatewayEvent) *logsv1.LogRecord {
	b, _ := json.Marshal(ev)
	attrs := []*commonv1.KeyValue{
		{Key: "event.type", Value: stringValue(ev.EventType)},
		{Key: "request.id", Value: stringValue(ev.RequestID)},
		{Key: "tenant.id", Value: stringValue(ev.TenantID)},
	}
	for k, v := range ev.Properties {
		attrs = append(attrs, &commonv1.KeyValue{Key: "gateway." + sanitizeAttr(k), Value: stringValue(v)})
	}
	for k, v := range ev.Error {
		attrs = append(attrs, &commonv1.KeyValue{Key: "error." + sanitizeAttr(k), Value: stringValue(v)})
	}
	sev := logsv1.SeverityNumber_SEVERITY_NUMBER_INFO
	if len(ev.Error) > 0 || strings.Contains(ev.EventType, "failed") || strings.Contains(ev.EventType, "error") {
		sev = logsv1.SeverityNumber_SEVERITY_NUMBER_ERROR
	}
	return &logsv1.LogRecord{
		TimeUnixNano:         uint64(ev.TimestampUnixNano),
		ObservedTimeUnixNano: uint64(time.Now().UnixNano()),
		SeverityNumber:       sev,
		SeverityText:         strings.ToUpper(strings.TrimPrefix(sev.String(), "SEVERITY_NUMBER_")),
		Body:                 stringValue(string(b)),
		Attributes:           attrs,
		EventName:            ev.EventType,
	}
}

func stringValue(v string) *commonv1.AnyValue {
	return &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: v}}
}

func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func stringMap(v any) map[string]string {
	out := map[string]string{}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case float64:
			out[k] = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(x)
		}
	}
	return out
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-otel",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "otel", Version: "0.1.0"}},
		},
		Schema:     `{"type":"object","properties":{"endpoint":{"type":"string"},"service_name":{"type":"string"},"insecure":{"type":"boolean"},"headers":{"type":"object"},"resource_attributes":{"type":"object"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
