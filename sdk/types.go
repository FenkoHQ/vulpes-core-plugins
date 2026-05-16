package sdk

import "time"

type CapabilityType string

const (
	CapabilityAuthenticator         CapabilityType = "authenticator"
	CapabilityAuthorizer            CapabilityType = "authorizer"
	CapabilityRateLimiter           CapabilityType = "rate_limiter"
	CapabilityRouter                CapabilityType = "router"
	CapabilityUpstreamProvider      CapabilityType = "upstream_provider"
	CapabilityCacheProvider         CapabilityType = "cache_provider"
	CapabilityObserver              CapabilityType = "observer"
	CapabilityPromptProvider        CapabilityType = "prompt_provider"
	CapabilityCostProvider          CapabilityType = "cost_provider"
	CapabilityModelRegistryProvider CapabilityType = "model_registry_provider"
)

type CallContext struct {
	RequestID      string
	TenantID       string
	Deadline       time.Time
	TraceContext   map[string]string
	GatewayVersion string
	PluginInstance string
}

type Diagnostic struct {
	Severity string            `json:"severity"`
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details,omitempty"`
}

type Metadata struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Homepage     string                 `json:"homepage,omitempty"`
	Capabilities []CapabilityDescriptor `json:"capabilities"`
	Permissions  Permissions            `json:"permissions"`
}

type CapabilityDescriptor struct {
	Type    CapabilityType `json:"type"`
	Name    string         `json:"name"`
	Version string         `json:"version"`
}

type Permissions struct {
	OutboundHosts []string        `json:"outbound_hosts"`
	SecretNames   []string        `json:"secret_names"`
	Data          DataPermissions `json:"data"`
}

type DataPermissions struct {
	ReadPrompt     bool `json:"read_prompt"`
	ReadResponse   bool `json:"read_response"`
	ModifyRequest  bool `json:"modify_request"`
	ModifyResponse bool `json:"modify_response"`
	ReadHeaders    bool `json:"read_headers"`
}

type HandshakeRequest struct {
	GatewayVersion            string
	SupportedProtocolVersions []int
}

type HandshakeResponse struct {
	SelectedProtocolVersion int
	PluginName              string
	PluginVersion           string
	Diagnostics             []Diagnostic
}

type ConfigureRequest struct {
	Context         CallContext
	ConfigJSON      string
	ResolvedSecrets map[string]string
}

type ConfigureResponse struct{ Diagnostics []Diagnostic }
type HealthRequest struct{}
type HealthResponse struct {
	State       string
	Diagnostics []Diagnostic
}
type GetConfigSchemaRequest struct{}
type GetConfigSchemaResponse struct{ SchemaJSON string }
type GetMetadataRequest struct{}
type ShutdownRequest struct{}
type ShutdownResponse struct{}

type Identity struct {
	Subject    string            `json:"subject"`
	TenantID   string            `json:"tenant_id"`
	Groups     []string          `json:"groups,omitempty"`
	Claims     map[string]string `json:"claims,omitempty"`
	AuthMethod string            `json:"auth_method"`
}

type RequestSummary struct {
	Operation            string            `json:"operation"`
	RequestedModel       string            `json:"requested_model"`
	EstimatedInputTokens int64             `json:"estimated_input_tokens"`
	Properties           map[string]string `json:"properties,omitempty"`
}

type RequestConstraints struct {
	AllowedModels       []string `json:"allowed_models,omitempty"`
	MaxOutputTokens     int64    `json:"max_output_tokens,omitempty"`
	MaxEstimatedCostUSD float64  `json:"max_estimated_cost_usd,omitempty"`
	AllowedRegions      []string `json:"allowed_regions,omitempty"`
}

type Usage struct {
	ProviderInstance string  `json:"provider_instance,omitempty"`
	ProviderModel    string  `json:"provider_model,omitempty"`
	InputTokens      int64   `json:"input_tokens,omitempty"`
	OutputTokens     int64   `json:"output_tokens,omitempty"`
	TotalTokens      int64   `json:"total_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

type ChatMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type ChatCompletionRequest struct {
	Model       string         `json:"model"`
	Messages    []ChatMessage  `json:"messages"`
	Stream      bool           `json:"stream,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int64         `json:"max_tokens,omitempty"`
	Tools       any            `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Metadata    any            `json:"metadata,omitempty"`
	Extra       map[string]any `json:"-"`
}

type ChatCompletionResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []ChatChoice     `json:"choices"`
	Usage   map[string]int64 `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message,omitempty"`
	Delta        ChatMessage `json:"delta,omitempty"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type ChatCompletionChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
}

type UpstreamError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	HTTPStatus  int    `json:"http_status"`
	Retryable   bool   `json:"retryable"`
	RateLimited bool   `json:"rate_limited"`
}

type ResponseChunk struct {
	Chunk *ChatCompletionChunk `json:"chunk,omitempty"`
	Usage *Usage               `json:"usage,omitempty"`
	Error *UpstreamError       `json:"error,omitempty"`
}

type AuthenticateRequest struct {
	Context  CallContext       `json:"context"`
	Headers  map[string]string `json:"headers"`
	SourceIP string            `json:"source_ip"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
}

type AuthenticateResponse struct {
	Allow       bool         `json:"allow"`
	Identity    Identity     `json:"identity"`
	DenyReason  string       `json:"deny_reason,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type RateLimitCheckRequest struct {
	Context               CallContext       `json:"context"`
	Identity              Identity          `json:"identity"`
	Model                 string            `json:"model"`
	EstimatedInputTokens  int64             `json:"estimated_input_tokens"`
	RequestedOutputTokens int64             `json:"requested_output_tokens"`
	Properties            map[string]string `json:"properties,omitempty"`
}

type RateLimitState struct {
	RequestLimit       int64   `json:"request_limit,omitempty"`
	RequestRemaining   int64   `json:"request_remaining,omitempty"`
	TokenLimit         int64   `json:"token_limit,omitempty"`
	TokenRemaining     int64   `json:"token_remaining,omitempty"`
	BudgetLimitUSD     float64 `json:"budget_limit_usd,omitempty"`
	BudgetRemainingUSD float64 `json:"budget_remaining_usd,omitempty"`
	ResetUnixNano      int64   `json:"reset_unix_nano,omitempty"`
}

type RateLimitCheckResponse struct {
	Decision     string         `json:"decision"`
	DenyReason   string         `json:"deny_reason,omitempty"`
	RetryAfterMS int64          `json:"retry_after_ms,omitempty"`
	State        RateLimitState `json:"state,omitempty"`
}

type CommitUsageRequest struct {
	Context  CallContext `json:"context"`
	Identity Identity    `json:"identity"`
	Usage    Usage       `json:"usage"`
}

type RouteCandidate struct {
	ProviderInstance         string            `json:"provider_instance"`
	ProviderModel            string            `json:"provider_model"`
	LogicalModel             string            `json:"logical_model"`
	Weight                   int               `json:"weight"`
	Region                   string            `json:"region,omitempty"`
	Healthy                  bool              `json:"healthy"`
	EstimatedCostPer1KInput  float64           `json:"estimated_cost_per_1k_input,omitempty"`
	EstimatedCostPer1KOutput float64           `json:"estimated_cost_per_1k_output,omitempty"`
	Properties               map[string]string `json:"properties,omitempty"`
}

type RouteRequest struct {
	Context        CallContext        `json:"context"`
	Identity       Identity           `json:"identity"`
	RequestedModel string             `json:"requested_model"`
	Request        RequestSummary     `json:"request"`
	Candidates     []RouteCandidate   `json:"candidates"`
	Constraints    RequestConstraints `json:"constraints,omitempty"`
}

type SelectedRoute struct {
	ProviderInstance string            `json:"provider_instance"`
	ProviderModel    string            `json:"provider_model"`
	Priority         int               `json:"priority"`
	Properties       map[string]string `json:"properties,omitempty"`
}

type RouteResponse struct {
	Routes      []SelectedRoute `json:"routes"`
	Reason      string          `json:"reason,omitempty"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

type InvokeRequest struct {
	Context       CallContext           `json:"context"`
	Identity      Identity              `json:"identity"`
	ProviderModel string                `json:"provider_model"`
	Request       ChatCompletionRequest `json:"request"`
	Properties    map[string]string     `json:"properties,omitempty"`
}

type ModelInfo struct {
	ID                string            `json:"id"`
	Object            string            `json:"object,omitempty"`
	OwnedBy           string            `json:"owned_by,omitempty"`
	ProviderInstance  string            `json:"provider_instance,omitempty"`
	ProviderModel     string            `json:"provider_model,omitempty"`
	ContextWindow     int64             `json:"context_window,omitempty"`
	SupportsStreaming bool              `json:"supports_streaming,omitempty"`
	SupportsTools     bool              `json:"supports_tools,omitempty"`
	SupportsJSONMode  bool              `json:"supports_json_mode,omitempty"`
	SupportsVision    bool              `json:"supports_vision,omitempty"`
	Region            string            `json:"region,omitempty"`
	Healthy           bool              `json:"healthy"`
	Properties        map[string]string `json:"properties,omitempty"`
}

type CachePolicy struct {
	TTLMillis int64             `json:"ttl_ms,omitempty"`
	Semantic  bool              `json:"semantic,omitempty"`
	Vary      map[string]string `json:"vary,omitempty"`
}

type CachedResponse struct {
	Response ChatCompletionResponse `json:"response"`
	Usage    Usage                  `json:"usage,omitempty"`
}

type CacheLookupRequest struct {
	Context  CallContext `json:"context"`
	Identity Identity    `json:"identity"`
	CacheKey string      `json:"cache_key"`
	Policy   CachePolicy `json:"policy"`
}

type CacheLookupResponse struct {
	Hit      bool           `json:"hit"`
	Response CachedResponse `json:"response,omitempty"`
	CacheID  string         `json:"cache_id,omitempty"`
}

type CacheStoreRequest struct {
	Context  CallContext    `json:"context"`
	Identity Identity       `json:"identity"`
	CacheKey string         `json:"cache_key"`
	Response CachedResponse `json:"response"`
	Policy   CachePolicy    `json:"policy"`
}

type GatewayEvent struct {
	EventID           string            `json:"event_id"`
	RequestID         string            `json:"request_id"`
	TenantID          string            `json:"tenant_id,omitempty"`
	EventType         string            `json:"event_type"`
	TimestampUnixNano int64             `json:"timestamp_unix_nano"`
	Properties        map[string]string `json:"properties,omitempty"`
	Usage             Usage             `json:"usage,omitempty"`
	Error             map[string]string `json:"error,omitempty"`
}

type PromptResolveRequest struct {
	Context   CallContext       `json:"context"`
	Identity  Identity          `json:"identity"`
	PromptRef string            `json:"prompt_ref"`
	Version   string            `json:"version,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

type PromptResolveResponse struct {
	Messages        []ChatMessage     `json:"messages"`
	ResolvedVersion string            `json:"resolved_version,omitempty"`
	Properties      map[string]string `json:"properties,omitempty"`
}
