// Package httpnpipe provides a HTTP transport (net/http.RoundTripper)
// that uses named pipes rather than sockets for HTTP.
//
// The URLs are of the form:
//
//     http+npipe://SERVICE/PATH_ETC
//
// SERVICE is utilized to map to the correct named pipe.
// Transport.RegisterTargetService, and PATH_ETC follow normal http: scheme
// conventions.
package httpnpipe

import (
	"bufio"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/docker/go-connections/sockets"
)

// Scheme is the URL scheme used for HTTP over named pipes.
const Scheme = "http+npipe"

// Transport is a http.RoundTripper that connects to named pipes.

type Transport struct {
	DialTimeout           time.Duration
	RequestTimeout        time.Duration
	ResponseHeaderTimeout time.Duration

	mutex sync.Mutex
	// map a URL "hostname" to a named pipe
	pipeMapping map[string]string
}

// RegisterTargetService registers a service name (URL) and maps it to target
// named pipe.
// This function is invoked in the client wishing to connect to a given
// service over named pipes.
//
// Calling RegisterTargetService twice for the same service is a
// programmer error, and causes a panic.
func (transport *Transport) RegisterTargetService(serviceName string, pipeName string) {
	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	if transport.pipeMapping == nil {
		transport.pipeMapping = make(map[string]string)
	}
	if _, exists := transport.pipeMapping[serviceName]; exists {
		panic("service " + serviceName + " already registered")
	}
	transport.pipeMapping[serviceName] = pipeName
}

var _ http.RoundTripper = (*Transport)(nil)

// RoundTrip executes a single HTTP transaction. See
// net/http.RoundTripper.
func (transport *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, errors.New("http+npipe: nil Request.URL")
	}
	if req.URL.Scheme != Scheme {
		return nil, errors.New("unsupported protocol scheme: " + req.URL.Scheme)
	}
	if req.URL.Host == "" {
		return nil, errors.New("http+npipe: no Host in request URL")
	}

	transport.mutex.Lock()
	pipeName, ok := transport.pipeMapping[req.URL.Host]
	transport.mutex.Unlock()
	if !ok {
		return nil, errors.New("unknown service: " + req.Host)
	}

	c, err := sockets.DialPipe(pipeName, transport.DialTimeout)
	if err != nil {
		return nil, err
	}

	r := bufio.NewReader(c)
	if transport.RequestTimeout > 0 {
		c.SetWriteDeadline(time.Now().Add(transport.RequestTimeout))
	}

	if err := req.Write(c); err != nil {
		return nil, err
	}

	if transport.ResponseHeaderTimeout > 0 {
		c.SetReadDeadline(time.Now().Add(transport.ResponseHeaderTimeout))
	}

	resp, err := http.ReadResponse(r, req)
	return resp, err
}
