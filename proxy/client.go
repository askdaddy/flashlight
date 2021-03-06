package proxy

import (
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/getlantern/enproxy"
	"github.com/getlantern/flashlight/log"
)

const (
	CONNECT = "CONNECT" // HTTP CONNECT method

	REVERSE_PROXY_FLUSH_INTERVAL = 250 * time.Millisecond
)

type Client struct {
	ProxyConfig

	EnproxyConfig *enproxy.Config

	reverseProxy *httputil.ReverseProxy
}

func (client *Client) Run() error {
	client.buildReverseProxy()

	httpServer := &http.Server{
		Addr:         client.Addr,
		ReadTimeout:  client.ReadTimeout,
		WriteTimeout: client.WriteTimeout,
		Handler:      client,
	}

	log.Debugf("About to start client (http) proxy at %s", client.Addr)
	return httpServer.ListenAndServe()
}

func (client *Client) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	log.Debugf("Handling request for: %s", req.RequestURI)
	if req.Method == CONNECT {
		client.EnproxyConfig.Intercept(resp, req)
	} else {
		client.reverseProxy.ServeHTTP(resp, req)
	}
}

// buildReverseProxy builds the httputil.ReverseProxy used by the client to
// proxy requests upstream.
func (client *Client) buildReverseProxy() {
	client.reverseProxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// do nothing
		},
		Transport: withDumpHeaders(
			client.ShouldDumpHeaders,
			&http.Transport{
				// We disable keepalives because some servers pretend to support
				// keep-alives but close their connections immediately, which
				// causes an error inside ReverseProxy.  This is not an issue
				// for HTTPS because  the browser is responsible for handling
				// the problem, which browsers like Chrome and Firefox already
				// know to do.
				// See https://code.google.com/p/go/issues/detail?id=4677
				DisableKeepAlives: true,
				Dial: func(network, addr string) (net.Conn, error) {
					conn := &enproxy.Conn{
						Addr:   addr,
						Config: client.EnproxyConfig,
					}
					err := conn.Connect()
					if err != nil {
						return nil, err
					}
					return conn, nil
				},
			}),
		// Set a FlushInterval to prevent overly aggressive buffering of
		// responses, which helps keep memory usage down
		FlushInterval: 250 * time.Millisecond,
	}
}

// withDumpHeaders creates a RoundTripper that uses the supplied RoundTripper
// and that dumps headers (if dumpHeaders is true).
func withDumpHeaders(dumpHeaders bool, rt http.RoundTripper) http.RoundTripper {
	if !dumpHeaders {
		return rt
	}
	return &headerDumpingRoundTripper{rt}
}

// headerDumpingRoundTripper is an http.RoundTripper that wraps another
// http.RoundTripper and dumps response headers to the log.
type headerDumpingRoundTripper struct {
	orig http.RoundTripper
}

func (rt *headerDumpingRoundTripper) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	dumpHeaders("Request", &req.Header)
	resp, err = rt.orig.RoundTrip(req)
	if err == nil {
		dumpHeaders("Response", &resp.Header)
	}
	return
}
