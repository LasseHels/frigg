package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

type Server struct {
	address string
	logger  *slog.Logger
	server  *http.Server
	router  *mux.Router
}

// New returns an initialized, but un-started Server.
func New(cfg Config, logger *slog.Logger) *Server {
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	router := mux.NewRouter()
	timeout := 5 * time.Second

	s := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadTimeout:       timeout,
		ReadHeaderTimeout: timeout,
		WriteTimeout:      timeout,
		IdleTimeout:       timeout,
	}

	return &Server{
		address: address,
		logger:  logger,
		server:  s,
		router:  router,
	}
}

func (s *Server) Start() error {
	s.logger.Info("Starting server", slog.String("address", s.address))

	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *Server) Stop() error {
	s.logger.Info("Stopping server")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}
	s.logger.Info("Stopped server")

	return nil
}

type Route struct {
	Path    string
	Methods []string
	Func    http.HandlerFunc
}

// RegisterRoute on the Server. All routes should be registered before Start is called.
func (s *Server) RegisterRoute(r Route) {
	s.logger.Info("Registered route", slog.String("path", r.Path), slog.Any("methods", r.Methods))
	s.router.HandleFunc(r.Path, r.Func).Methods(r.Methods...)
}
