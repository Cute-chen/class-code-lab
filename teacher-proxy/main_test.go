package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRunnerListenFollowsMainPort(t *testing.T) {
	for _, tt := range []struct{ main, runner string }{
		{":8088", ":8089"},
		{":9088", ":9089"},
		{"0.0.0.0:8088", "0.0.0.0:8089"},
	} {
		got, err := nextListenAddr(tt.main)
		if err != nil || got != tt.runner {
			t.Fatalf("nextListenAddr(%q) = %q, %v; want %q", tt.main, got, err, tt.runner)
		}
	}
	if _, err := nextListenAddr(":65535"); err == nil {
		t.Fatal("expected invalid runner port to be rejected")
	}
}

func TestProxyForwardsMainAndRunner(t *testing.T) {
	upstreamMain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("main path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "main ok")
	}))
	defer upstreamMain.Close()
	upstreamRunner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/run/test-token" {
			t.Errorf("runner path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Security-Policy", "connect-src 'none'")
		_, _ = io.WriteString(w, "runner ok")
	}))
	defer upstreamRunner.Close()

	for _, tt := range []struct {
		name, upstream, path, body string
	}{
		{"main", upstreamMain.URL, "/api/health", "main ok"},
		{"runner", upstreamRunner.URL, "/run/test-token", "runner ok"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target, err := url.Parse(tt.upstream)
			if err != nil {
				t.Fatal(err)
			}
			proxy := newProxyServer(":0", target, nil, false, tt.name)
			resp := httptest.NewRecorder()
			proxy.Handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if resp.Code != http.StatusOK || strings.TrimSpace(resp.Body.String()) != tt.body {
				t.Fatalf("proxy response: %d %q", resp.Code, resp.Body.String())
			}
			if tt.name == "runner" && resp.Header().Get("Content-Security-Policy") != "connect-src 'none'" {
				t.Fatal("runner CSP was not forwarded")
			}
		})
	}
}
