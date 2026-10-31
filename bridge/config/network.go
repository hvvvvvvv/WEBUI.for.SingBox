package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"guiforcores/bridge/network"
	appv1 "guiforcores/gen/app/v1"

	"connectrpc.com/connect"
)

func (cfg AppConfig) NetworkOptions() network.Options {
	return network.Options{
		Mode: cfg.RequestProxyMode, ProxyURL: cfg.CustomProxy,
		UserAgent: cfg.UserAgent, GitHubToken: cfg.GitHubAPIToken, GitHubMirror: cfg.GitHubDownloadMirror,
	}
}

func (s *Store) NetworkOptions() network.Options { return s.Current().NetworkOptions() }

// SetNetworkOptions is configured once before serving requests and supplies the
// running core's proxy resolver in addition to the persisted request settings.
func (s *AppService) SetNetworkOptions(provider func() network.Options) { s.networkOptions = provider }

func (s *AppService) TestNetworkRequest(ctx context.Context, request *connect.Request[appv1.TestNetworkRequestRequest]) (*connect.Response[appv1.TestNetworkRequestResponse], error) {
	target := request.Msg.GetUrl()
	if target == "" {
		target = "https://www.gstatic.com/generate_204"
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || len(target) > 4096 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("test URL must be an HTTP(S) URL without credentials"))
	}
	options := s.store.NetworkOptions()
	if s.networkOptions != nil {
		options = s.networkOptions()
	}
	client, err := network.NewClient(options, 10*time.Second, false)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	defer client.CloseIdleConnections()
	requestHTTP, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := network.PrepareRequest(requestHTTP, options, false); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	started := time.Now()
	response, err := client.Do(requestHTTP)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("backend request failed: %w", err))
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10)); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("backend response failed: %w", err))
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("backend request returned HTTP %d", response.StatusCode))
	}
	return connect.NewResponse(&appv1.TestNetworkRequestResponse{StatusCode: uint32(response.StatusCode), DurationMs: uint64(time.Since(started).Milliseconds())}), nil
}
