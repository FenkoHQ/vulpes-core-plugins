package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/FenkoHQ/vulpes-core-plugins/sdk"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type plugin struct {
	mu sync.Mutex

	bucket         string
	prefix         string
	endpoint       string
	region         string
	accessKey      string
	secretKey      string
	sessionToken   string
	forcePathStyle bool
	storeEvents    bool
	client         *s3.Client
}

type transcriptObject struct {
	Version    string            `json:"version"`
	StoredAt   string            `json:"stored_at"`
	RequestID  string            `json:"request_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	EventType  string            `json:"event_type"`
	Timestamp  int64             `json:"timestamp_unix_nano"`
	Properties map[string]string `json:"properties,omitempty"`
	Usage      sdk.Usage         `json:"usage,omitempty"`
	Error      map[string]string `json:"error,omitempty"`
	Request    json.RawMessage   `json:"request,omitempty"`
	Response   json.RawMessage   `json:"response,omitempty"`
}

func (p *plugin) Configure(ctx context.Context, cfg map[string]any, secrets map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.bucket = stringValue(cfg["bucket"])
	p.prefix = strings.Trim(stringValue(cfg["prefix"]), "/")
	p.endpoint = stringValue(cfg["endpoint"])
	p.region = firstString(cfg["region"], "us-east-1")
	p.accessKey = firstString(cfg["access_key_id"], cfg["access_key"], secrets["S3_ACCESS_KEY_ID"], secrets["AWS_ACCESS_KEY_ID"])
	p.secretKey = firstString(cfg["secret_access_key"], cfg["secret_key"], secrets["S3_SECRET_ACCESS_KEY"], secrets["AWS_SECRET_ACCESS_KEY"])
	p.sessionToken = firstString(cfg["session_token"], secrets["AWS_SESSION_TOKEN"])
	p.forcePathStyle = true
	if v, ok := cfg["force_path_style"].(bool); ok {
		p.forcePathStyle = v
	}
	p.storeEvents = false
	if v, ok := cfg["store_all_events"].(bool); ok {
		p.storeEvents = v
	}
	if p.bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if p.accessKey == "" || p.secretKey == "" {
		return fmt.Errorf("access_key_id and secret_access_key are required")
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(p.region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(p.accessKey, p.secretKey, p.sessionToken)),
	}
	if p.endpoint != "" {
		opts = append(opts, config.WithBaseEndpoint(p.endpoint))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return err
	}
	p.client = s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = p.forcePathStyle })
	return nil
}

func (p *plugin) Emit(ctx context.Context, events []sdk.GatewayEvent) error {
	p.mu.Lock()
	client := p.client
	bucket := p.bucket
	prefix := p.prefix
	storeAll := p.storeEvents
	p.mu.Unlock()
	if client == nil {
		return nil
	}
	for _, ev := range events {
		if !storeAll && ev.EventType != "request.completed" && ev.EventType != "request.failed" {
			continue
		}
		obj := buildTranscript(ev)
		body, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		key := transcriptKey(prefix, ev)
		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(body),
			ContentType: aws.String("application/json"),
		})
		if err != nil {
			return fmt.Errorf("put transcript %s/%s: %w", bucket, key, err)
		}
	}
	return nil
}

func buildTranscript(ev sdk.GatewayEvent) transcriptObject {
	props := map[string]string{}
	for k, v := range ev.Properties {
		if k == "request_json" || k == "response_json" {
			continue
		}
		props[k] = v
	}
	obj := transcriptObject{Version: "gateway.fenko.dev/transcript.v1", StoredAt: time.Now().UTC().Format(time.RFC3339Nano), RequestID: ev.RequestID, TenantID: ev.TenantID, EventType: ev.EventType, Timestamp: ev.TimestampUnixNano, Properties: props, Usage: ev.Usage, Error: ev.Error}
	if raw := ev.Properties["request_json"]; raw != "" && json.Valid([]byte(raw)) {
		obj.Request = json.RawMessage(raw)
	}
	if raw := ev.Properties["response_json"]; raw != "" && json.Valid([]byte(raw)) {
		obj.Response = json.RawMessage(raw)
	}
	return obj
}

func transcriptKey(prefix string, ev sdk.GatewayEvent) string {
	t := time.Unix(0, ev.TimestampUnixNano).UTC()
	if ev.TimestampUnixNano == 0 {
		t = time.Now().UTC()
	}
	requestID := safePart(ev.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("event-%d", t.UnixNano())
	}
	parts := []string{prefix, t.Format("2006/01/02/15"), requestID + "." + safePart(ev.EventType) + ".json"}
	return strings.TrimPrefix(path.Join(parts...), "/")
}

func safePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func firstString(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func main() {
	p := &plugin{}
	s := &sdk.Service{
		Metadata: sdk.Metadata{
			Name:         "observer-s3-transcripts",
			Version:      "0.1.0",
			Capabilities: []sdk.CapabilityDescriptor{{Type: sdk.CapabilityObserver, Name: "s3-transcripts", Version: "0.1.0"}},
			Permissions:  sdk.Permissions{SecretNames: []string{"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}, Data: sdk.DataPermissions{ReadPrompt: true, ReadResponse: true}},
		},
		Schema:     `{"type":"object","required":["bucket"],"properties":{"bucket":{"type":"string"},"prefix":{"type":"string"},"endpoint":{"type":"string"},"region":{"type":"string"},"access_key_id":{"type":"string"},"secret_access_key":{"type":"string"},"session_token":{"type":"string"},"force_path_style":{"type":"boolean"},"store_all_events":{"type":"boolean"}}}`,
		Configurer: p,
		Observer:   p,
	}
	if err := sdk.ServeFromEnv(s); err != nil {
		panic(err)
	}
}
