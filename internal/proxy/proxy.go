package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/YUYUEYUER/prompt-guard/internal/config"
)

type Service struct {
	proxy *httputil.ReverseProxy
}

func New(cfg config.UpstreamConfig, logger *slog.Logger) (*Service, error) {
	target, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	timeout, err := config.ParseDuration(cfg.Timeout)
	if err != nil {
		return nil, err
	}
	keepAlive, err := config.ParseDuration(cfg.KeepAlive)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: keepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       keepAlive,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("upstream proxy error", slog.String("error", err.Error()))
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"type":"upstream_unavailable","message":"upstream gateway unavailable"}}`))
	}

	return &Service{proxy: proxy}, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}
