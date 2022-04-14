/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/klog/v2"
)

// DefaultBindAddress sets the default bind address for the metrics listener
var DefaultBindAddress = ":8888"

const defaultMetricsEndpoint = "/metrics"

type Server struct {
	httpServer http.Server
	listener   net.Listener
}

func NewServer() (*Server, error) {
	listener, err := newListener(DefaultBindAddress)
	if err != nil {
		return nil, err
	}

	handler := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
	mux := http.NewServeMux()
	mux.Handle(defaultMetricsEndpoint, handler)

	return &Server{
		listener: listener,
		httpServer: http.Server{
			Handler: mux,
		},
	}, nil
}

func (s *Server) Start() error {
	klog.InfoS("Started serving metrics", "address", s.listener.Addr())
	if err := s.httpServer.Serve(s.listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown() error {
	klog.Info("Stopping metrics server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// newListener creates a new TCP listener bound to the given address.
func newListener(addr string) (net.Listener, error) {
	if addr == "" {
		// If the metrics bind address is empty, use the default one
		addr = DefaultBindAddress
	}

	// Add a case to disable metrics altogether
	if addr == "0" {
		return nil, nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	return listener, nil
}
