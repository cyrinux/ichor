// Command ichor-relay runs in the cluster, receives Alertmanager's webhook and pushes the
// alerts to the phones paired from Ichor: through UnifiedPush, encrypted end to end (RFC 8291
// Web Push, VAPID-signed), or as a plain ntfy publish.
//
// Its state (VAPID key pair, pairing and webhook tokens, paired devices) lives in one Secret,
// created on first start. Its ServiceAccount needs get, create and update on that Secret. Run
// one replica: a replica reads the Secret at start and after its own write conflicts only, so
// a second one would miss the devices paired through the first, and dedupe is in memory.
//
// Environment (flags of the same meaning override it):
//
//	RELAY_LISTEN     listen address (default :8080)
//	RELAY_NAMESPACE  namespace of the Secret (default: the ServiceAccount's)
//	RELAY_SECRET     name of the Secret (default ichor-relay)
//	RELAY_KUBE_API   API server URL without credentials, for a local run behind `kubectl proxy`
//	RELAY_ALLOW_HTTP true accepts plain-http push endpoints (an in-cluster ntfy); https only otherwise
//
// Endpoints: GET /healthz; GET /v1/pairing, PUT /v1/devices and DELETE /v1/devices/{id} with the
// X-Ichor-Pairing header (reached through the API server's service proxy); POST /alertmanager
// with "Authorization: Bearer <webhookToken>". Logs carry device IDs and HTTP statuses, never a
// token, a push endpoint or a label.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cyrinux/ichor/go/relay"
)

const shutdownTimeout = 20 * time.Second

type config struct {
	listen, namespace, secret, kubeAPI string
	allowHTTP                          bool
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("ichor-relay stopped", "err", err)
		os.Exit(1)
	}
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("ichor-relay", flag.ContinueOnError)
	c := config{}
	fs.StringVar(&c.listen, "listen", cmp.Or(os.Getenv("RELAY_LISTEN"), ":8080"), "listen address")
	fs.StringVar(&c.namespace, "namespace", os.Getenv("RELAY_NAMESPACE"), "namespace of the Secret")
	fs.StringVar(&c.secret, "secret", cmp.Or(os.Getenv("RELAY_SECRET"), "ichor-relay"), "name of the Secret")
	fs.StringVar(&c.kubeAPI, "kube-api", os.Getenv("RELAY_KUBE_API"), "API server URL without credentials (kubectl proxy)")
	fs.BoolVar(&c.allowHTTP, "allow-http", os.Getenv("RELAY_ALLOW_HTTP") == "true", "accept plain-http push endpoints")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if c.namespace == "" {
		ns, err := os.ReadFile(relay.SANamespaceFile)
		if err != nil {
			return config{}, errors.New("RELAY_NAMESPACE is unset and there is no ServiceAccount namespace file")
		}
		c.namespace = strings.TrimSpace(string(ns))
	}

	return c, nil
}

func run(log *slog.Logger, args []string) error {
	c, err := parseConfig(args)
	if err != nil {
		return err
	}

	kube := relay.LocalKube(c.kubeAPI)
	if c.kubeAPI == "" {
		if kube, err = relay.InClusterKube(); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	store := relay.NewStore(kube, c.namespace, c.secret)
	loadCtx, cancel := context.WithTimeout(ctx, time.Minute)
	err = store.Load(loadCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("load the state: %w", err)
	}
	log.Info("state loaded", "namespace", c.namespace, "secret", c.secret, "devices", len(store.Snapshot().Devices))

	r := relay.New(relay.Options{Store: store, Logger: log, AllowHTTP: c.allowHTTP})
	r.Start()

	srv := &http.Server{
		Addr:              c.listen,
		Handler:           r.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("listening", "addr", c.listen)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return errors.Join(srv.Shutdown(shutdownCtx), r.Close(shutdownCtx))
}
