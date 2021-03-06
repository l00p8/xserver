package xserver

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/valve"
	logger "github.com/l00p8/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config describes server configuration
type Config struct {
	Addr            string        `envconfig:"addr" mapstructure:"addr" default:":8080"`
	ShutdownTimeout time.Duration `envconfig:"shutdown_timeout" mapstructure:"shutdown_timeout" default:"20"`
	GracefulTimeout time.Duration `envconfig:"graceful_timeout" mapstructure:"graceful_timeout" default:"21"`
	HealthUri       string        `envconfig:"health_uri" mapstructure:"health_uri" default:"/_health"`
	ApiVersion      string        `envconfig:"api_version" mapstructure:"api_version" default:"v1"`
	Timeout         time.Duration `envconfig:"timeout" mapstructure:"timeout" default:"20"`
	RateLimit       int64         `envconfig:"rate_limit" mapstructure:"rate_limit" default:"1000"`
	CertPath        string        `envconfig:"cert_path" mapstructure:"cert_path" default:""`
	KeyPath         string        `envconfig:"key_path" mapstructure:"key_path" default:""`
	TLSEnabled      bool          `envconfig:"tls_enabled" mapstructure:"tls_enabled" default:""`
	Logger          logger.Logger
}

// Listen starts a http server on specified address and defines gateway routes
// Server implements a graceful shutdown pattern for better handling of rolling k8s updates
func Listen(cfg Config, router Muxer, cleanUp func()) error {
	valv := valve.New()
	log := cfg.Logger

	router.Mux().Handle("/_metrics", promhttp.Handler())

	srv := http.Server{
		Addr:         cfg.Addr,
		Handler:      router.Mux(),
		ReadTimeout:  2 * cfg.Timeout,
		WriteTimeout: 2 * cfg.Timeout,
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, os.Interrupt)

	go func() {
		<-c
		//for range c {
		// sig is a ^C, handle it
		log.Info("Shutting down a http server...")

		shutdown := cfg.ShutdownTimeout

		// first valv
		if err := valv.Shutdown(shutdown); err != nil {
			log.Error("Error shutting down a Valve: " + err.Error())
			return
		}

		// create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), shutdown)
		defer func() {
			signal.Stop(c)
			cancel()
		}()

		// cleanUp before shutDown
		cleanUp()

		// start http server shutdown
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("Error shutting down a http server: " + err.Error())
			return
		}

		// verify, in worst case call cancel via defer
		select {
		case <-time.After(cfg.GracefulTimeout):
			log.Info("Not all connections are done")
		case <-ctx.Done():

		}
		//}
	}()

	log.Info("Starting a new server on address: " + cfg.Addr)

	if !cfg.TLSEnabled {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Error("A server listener error: " + err.Error())
			return err
		}
	} else {
		if err := srv.ListenAndServeTLS(cfg.CertPath, cfg.KeyPath); err != http.ErrServerClosed {
			log.Error("A tls server listener error: " + err.Error())
			return err
		}
	}
	log.Info("Server is down")
	return nil
}
