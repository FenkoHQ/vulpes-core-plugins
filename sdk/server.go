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
	"syscall"
)

var ErrNotImplemented = errors.New("capability not implemented")

type Configurer interface {
	Configure(context.Context, map[string]any, map[string]string) error
}
type Authenticator interface {
	Authenticate(context.Context, AuthenticateRequest) (AuthenticateResponse, error)
}
type Router interface {
	Route(context.Context, RouteRequest) (RouteResponse, error)
}
type UpstreamProvider interface {
	Invoke(context.Context, InvokeRequest) ([]ResponseChunk, error)
	ListModels(context.Context) ([]ModelInfo, error)
}
type Observer interface {
	Emit(context.Context, []GatewayEvent) error
}

type Service struct {
	Metadata Metadata
	Schema   string

	Configurer       Configurer
	Authenticator    Authenticator
	Router           Router
	UpstreamProvider UpstreamProvider
	Observer         Observer
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
	ctx := contextFromCall(req.Context)
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
	out, err := s.Authenticator.Authenticate(contextFromCall(req.Context), req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) Route(req RouteRequest, resp *RouteResponse) error {
	if s.Router == nil {
		return ErrNotImplemented
	}
	out, err := s.Router.Route(contextFromCall(req.Context), req)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *Service) Invoke(req InvokeRequest, resp *[]ResponseChunk) error {
	if s.UpstreamProvider == nil {
		return ErrNotImplemented
	}
	out, err := s.UpstreamProvider.Invoke(contextFromCall(req.Context), req)
	if err != nil {
		return err
	}
	*resp = out
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

func (s *Service) Emit(events []GatewayEvent, resp *struct{}) error {
	if s.Observer == nil {
		return ErrNotImplemented
	}
	return s.Observer.Emit(context.Background(), events)
}

func contextFromCall(c CallContext) context.Context {
	return context.Background()
}
