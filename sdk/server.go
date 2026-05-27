package sdk

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

var ErrNotImplemented = errors.New("capability not implemented")

type Configurer interface {
	Configure(context.Context, map[string]any, map[string]string) error
}
type Authenticator interface {
	Authenticate(context.Context, AuthenticateRequest) (AuthenticateResponse, error)
}
type RateLimiter interface {
	Check(context.Context, RateLimitCheckRequest) (RateLimitCheckResponse, error)
	Commit(context.Context, CommitUsageRequest) error
}
type Router interface {
	Route(context.Context, RouteRequest) (RouteResponse, error)
}

// UpstreamProvider streams response chunks for an upstream call. The plugin
// owns `out`: it must send each chunk as it becomes available and then return
// (the SDK closes the channel for it). Errors that should surface on the
// in-flight request must be emitted as a ResponseChunk{Error: ...} rather than
// returned, so the gateway records the failure on the originating request.
// Returning a non-nil error fails the InvokeStart RPC itself — use it only
// for setup failures (validation, missing config) that happen before any
// chunks have been produced.
type UpstreamProvider interface {
	Invoke(ctx context.Context, req InvokeRequest, out chan<- ResponseChunk) error
	ListModels(ctx context.Context) ([]ModelInfo, error)
}
type CacheProvider interface {
	Lookup(context.Context, CacheLookupRequest) (CacheLookupResponse, error)
	Store(context.Context, CacheStoreRequest) error
}
type Observer interface {
	Emit(context.Context, []GatewayEvent) error
}
type PromptProvider interface {
	ResolvePrompt(context.Context, PromptResolveRequest) (PromptResolveResponse, error)
}

type Service struct {
	Metadata Metadata
	Schema   string

	Configurer       Configurer
	Authenticator    Authenticator
	RateLimiter      RateLimiter
	Router           Router
	UpstreamProvider UpstreamProvider
	CacheProvider    CacheProvider
	Observer         Observer
	PromptProvider   PromptProvider

	streams sync.Map // streamID -> *invokeStream
	nextID  atomic.Uint64
}

// invokeStream is the in-flight state of one upstream call. The producer
// goroutine pushes chunks onto ch; cancel aborts the underlying request
// (e.g. cancels the upstream HTTP). The SDK closes ch when the producer
// returns. InvokeNext readers consume ch — a closed ch means EOF.
type invokeStream struct {
	ch     chan ResponseChunk
	cancel context.CancelFunc
}

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
	gob.Register("")
	gob.Register(float64(0))
	gob.Register(int64(0))
	gob.Register(bool(false))
}

func ServeFromEnv(service *Service) error {
	sock := os.Getenv("GATEWAY_PLUGIN_SOCKET")
	if sock == "" {
		return fmt.Errorf("GATEWAY_PLUGIN_SOCKET is required")
	}
	return ServeUnix(context.Background(), sock, service)
}

func ServeUnix(ctx context.Context, socketPath string, service *Service) error {
	if service == nil {
		return fmt.Errorf("nil service")
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", socketPath, err)
	}
	defer os.Remove(socketPath)
	defer ln.Close()

	srv := rpc.NewServer()
	if err := srv.RegisterName("Plugin", service); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					errCh <- nil
				default:
					errCh <- err
				}
				return
			}
			go srv.ServeConn(conn)
		}
	}()

	select {
	case <-ctx.Done():
		_ = ln.Close()
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Service) Handshake(req HandshakeRequest, resp *HandshakeResponse) error {
	selected := 0
	for _, v := range req.SupportedProtocolVersions {
		if v == 1 {
			selected = 1
			break
		}
	}
	if selected == 0 {
		return fmt.Errorf("no supported protocol version")
	}
	resp.SelectedProtocolVersion = selected
	resp.PluginName = s.Metadata.Name
	resp.PluginVersion = s.Metadata.Version
	return nil
}

func (s *Service) GetMetadata(req GetMetadataRequest, resp *Metadata) error {
	*resp = s.Metadata
	return nil
}

func (s *Service) GetConfigSchema(req GetConfigSchemaRequest, resp *GetConfigSchemaResponse) error {
	resp.SchemaJSON = s.Schema
	return nil
}

func (s *Service) Configure(req ConfigureRequest, resp *ConfigureResponse) error {
	if s.Configurer == nil {
		return nil
	}
	var cfg map[string]any
	if req.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(req.ConfigJSON), &cfg); err != nil {
			return err
		}
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	return s.Configurer.Configure(ctx, cfg, req.ResolvedSecrets)
}

func (s *Service) Health(req HealthRequest, resp *HealthResponse) error {
	resp.State = "ready"
	return nil
}

func (s *Service) Shutdown(req ShutdownRequest, resp *ShutdownResponse) error { return nil }

func (s *Service) Authenticate(req AuthenticateRequest, resp *AuthenticateResponse) error {
	if s.Authenticator == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	out, err := s.Authenticator.Authenticate(ctx, req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) CheckRateLimit(req RateLimitCheckRequest, resp *RateLimitCheckResponse) error {
	if s.RateLimiter == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	out, err := s.RateLimiter.Check(ctx, req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) CommitUsage(req CommitUsageRequest, resp *struct{}) error {
	if s.RateLimiter == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	return s.RateLimiter.Commit(ctx, req)
}

func (s *Service) Route(req RouteRequest, resp *RouteResponse) error {
	if s.Router == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	out, err := s.Router.Route(ctx, req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

// InvokeStart spins up a streaming upstream call. It returns immediately with
// a stream ID; the caller pulls chunks via InvokeNext until eof=true. The
// producer goroutine runs the plugin's UpstreamProvider.Invoke, which writes
// chunks to a buffered channel as they arrive — when that function returns,
// the SDK closes the channel so any waiting InvokeNext observes EOF.
func (s *Service) InvokeStart(req InvokeRequest, resp *InvokeStartResponse) error {
	if s.UpstreamProvider == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	// Buffer of 32 absorbs short bursts (token deltas) without blocking the
	// producer when the consumer is briefly slow — but stays small enough
	// that backpressure still flows back to the upstream parser.
	ch := make(chan ResponseChunk, 32)
	id := s.nextID.Add(1)
	stream := &invokeStream{ch: ch, cancel: cancel}
	s.streams.Store(id, stream)
	go func() {
		defer close(ch)
		defer cancel()
		if err := s.UpstreamProvider.Invoke(ctx, req, ch); err != nil {
			// A setup-time error is delivered to the consumer as a chunk
			// rather than dropped on the floor (the InvokeStart RPC has
			// already returned by the time the producer runs).
			select {
			case ch <- ResponseChunk{Error: &UpstreamError{Code: "upstream_invoke_failed", Message: err.Error(), HTTPStatus: 500, Retryable: true}}:
			case <-ctx.Done():
			}
		}
	}()
	resp.StreamID = id
	return nil
}

// InvokeNext returns the next chunk on a stream, blocking until one arrives.
// When the producer has finished, the channel is closed and InvokeNext
// returns with EOF=true (one final empty response). The caller is expected
// to keep calling until EOF.
func (s *Service) InvokeNext(req InvokeNextRequest, resp *InvokeNextResponse) error {
	v, ok := s.streams.Load(req.StreamID)
	if !ok {
		return fmt.Errorf("unknown stream %d", req.StreamID)
	}
	stream := v.(*invokeStream)
	chunk, ok := <-stream.ch
	if !ok {
		// Producer is done. Clean up so the streamID can't be reused.
		s.streams.Delete(req.StreamID)
		resp.EOF = true
		return nil
	}
	resp.Chunk = chunk
	return nil
}

// InvokeCancel aborts an in-flight stream. The gateway calls this when its
// own context is cancelled (client disconnect, deadline). The producer's
// context is cancelled, which propagates to the upstream HTTP request, which
// closes the chunk channel. Any in-flight InvokeNext returns EOF.
func (s *Service) InvokeCancel(req InvokeCancelRequest, resp *InvokeCancelResponse) error {
	v, ok := s.streams.LoadAndDelete(req.StreamID)
	if !ok {
		return nil
	}
	v.(*invokeStream).cancel()
	return nil
}

func (s *Service) ListModels(req struct{}, resp *[]ModelInfo) error {
	if s.UpstreamProvider == nil {
		*resp = nil
		return nil
	}
	out, err := s.UpstreamProvider.ListModels(context.Background())
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) CacheLookup(req CacheLookupRequest, resp *CacheLookupResponse) error {
	if s.CacheProvider == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	out, err := s.CacheProvider.Lookup(ctx, req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) CacheStore(req CacheStoreRequest, resp *struct{}) error {
	if s.CacheProvider == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	return s.CacheProvider.Store(ctx, req)
}

func (s *Service) Emit(events []GatewayEvent, resp *struct{}) error {
	if s.Observer == nil {
		return ErrNotImplemented
	}
	return s.Observer.Emit(context.Background(), events)
}

func (s *Service) ResolvePrompt(req PromptResolveRequest, resp *PromptResolveResponse) error {
	if s.PromptProvider == nil {
		return ErrNotImplemented
	}
	ctx, cancel := contextFromCall(req.Context)
	defer cancel()
	out, err := s.PromptProvider.ResolvePrompt(ctx, req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func contextFromCall(c CallContext) (context.Context, context.CancelFunc) {
	if !c.Deadline.IsZero() {
		return context.WithDeadline(context.Background(), c.Deadline)
	}
	return context.Background(), func() {}
}
