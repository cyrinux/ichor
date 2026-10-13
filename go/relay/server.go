package relay

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// MaxDevices bounds the paired phones.
	MaxDevices = 50
	// maxWebhookBody and maxDeviceBody bound the request bodies.
	maxWebhookBody = 1 << 20
	maxDeviceBody  = 16 << 10
	// defaultWorkers deliveries run at once, each bounded by defaultTimeout.
	defaultWorkers = 10
	defaultTimeout = 10 * time.Second
	// queueSize bounds the deliveries waiting for a worker; a webhook that does not fit is
	// refused with 503 so Alertmanager retries it.
	queueSize = 1000

	pairingHeader = "X-Ichor-Pairing"
)

// Options configure a Relay. Store is required; the rest have defaults.
type Options struct {
	Store *Store
	// Client sends the pushes.
	Client *http.Client
	Logger *slog.Logger
	Now    func() time.Time
	// Workers is the number of deliveries in flight; Timeout bounds each.
	Workers int
	Timeout time.Duration
}

// Relay serves the pairing API and the Alertmanager webhook, and delivers the pushes.
type Relay struct {
	store   *Store
	sender  sender
	dedupe  *dedupe
	log     *slog.Logger
	now     func() time.Time
	workers int
	timeout time.Duration

	// closing guards jobs: handlers enqueue under the read lock, Close closes under the write
	// lock, so a handler outliving the http.Server's shutdown never sends on a closed channel.
	closing sync.RWMutex
	closed  bool
	jobs    chan delivery
	wg      sync.WaitGroup
}

// delivery is one device's share of a webhook. The device is looked up again when it is sent,
// so a re-pairing or a removal since the webhook is honoured.
type delivery struct {
	deviceID string
	alerts   []PushAlert
}

// New returns a relay; Start runs its workers.
func New(o Options) *Relay {
	r := &Relay{
		store:   o.Store,
		log:     o.Logger,
		now:     o.Now,
		workers: o.Workers,
		timeout: o.Timeout,
		jobs:    make(chan delivery, queueSize),
	}
	if r.log == nil {
		r.log = slog.Default()
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.workers <= 0 {
		r.workers = defaultWorkers
	}
	if r.timeout <= 0 {
		r.timeout = defaultTimeout
	}

	client := o.Client
	if client == nil {
		client = newHTTPClient(nil, http.ProxyFromEnvironment, 0)
	}
	r.sender = sender{client: client, now: r.now}
	r.dedupe = newDedupe(r.now)

	return r
}

// Start runs the delivery workers until Close.
func (r *Relay) Start() {
	for range r.workers {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			for d := range r.jobs {
				r.deliver(d)
			}
		}()
	}
}

// Close stops taking deliveries (a webhook then gets 503) and waits for the queued ones, or
// for ctx.
func (r *Relay) Close(ctx context.Context) error {
	r.closing.Lock()
	if !r.closed {
		r.closed = true
		close(r.jobs)
	}
	r.closing.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Handler is the relay's HTTP API.
func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /v1/pairing", r.pairing(r.handlePairing))
	mux.HandleFunc("PUT /v1/devices", r.pairing(r.handlePutDevice))
	mux.HandleFunc("DELETE /v1/devices/{id}", r.pairing(r.handleDeleteDevice))
	mux.HandleFunc("POST /alertmanager", r.handleWebhook)

	return mux
}

// pairing guards the /v1 API with the pairing token. The phone reaches it through the API
// server's service proxy, which consumes the Authorization header, hence a header of its own.
func (r *Relay) pairing(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !tokenMatches(req.Header.Get(pairingHeader), r.store.Snapshot().PairingToken) {
			writeError(w, http.StatusUnauthorized, "missing or wrong pairing token")

			return
		}
		next(w, req)
	}
}

func (r *Relay) handlePairing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"vapidPublicKey": r.store.Snapshot().VAPIDPublicKey})
}

func (r *Relay) handlePutDevice(w http.ResponseWriter, req *http.Request) {
	var device Device
	if status, msg := decodeBody(w, req, maxDeviceBody, &device); status != 0 {
		writeError(w, status, msg)

		return
	}
	if err := validateDevice(device); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())

		return
	}

	err := r.store.Update(req.Context(), func(s *State) error {
		return upsertDevice(s, device, r.now())
	})
	switch {
	case errors.Is(err, errTooManyDevices):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case err != nil:
		r.log.Error("save device", "device", device.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not save the device")
	default:
		r.log.Info("device paired", "device", device.ID, "platform", device.Platform)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (r *Relay) handleDeleteDevice(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.removeDevice(req.Context(), id, ""); err != nil {
		r.log.Error("remove device", "device", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not remove the device")

		return
	}
	r.log.Info("device removed", "device", id)
	w.WriteHeader(http.StatusNoContent)
}

func (r *Relay) handleWebhook(w http.ResponseWriter, req *http.Request) {
	bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	if !ok || !tokenMatches(bearer, r.store.Snapshot().WebhookToken) {
		writeError(w, http.StatusUnauthorized, "missing or wrong webhook token")

		return
	}

	var msg WebhookMessage
	if status, errMsg := decodeBody(w, req, maxWebhookBody, &msg); status != 0 {
		writeError(w, status, errMsg)

		return
	}

	alerts := webhookAlerts(msg)
	devices := r.store.Snapshot().Devices
	if len(alerts) == 0 || len(devices) == 0 {
		w.WriteHeader(http.StatusAccepted)

		return
	}

	if !r.enqueue(devices, alerts) {
		writeError(w, http.StatusServiceUnavailable, "busy, retry later")

		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// enqueue queues one delivery per device; false when the relay is closing or the queue cannot
// take them all.
func (r *Relay) enqueue(devices []Device, alerts []PushAlert) bool {
	r.closing.RLock()
	defer r.closing.RUnlock()

	if r.closed {
		return false
	}
	if len(r.jobs)+len(devices) > cap(r.jobs) {
		r.log.Warn("delivery queue full, webhook refused", "alerts", len(alerts))

		return false
	}

	for _, d := range devices {
		select {
		case r.jobs <- delivery{deviceID: d.ID, alerts: alerts}:
		default:
			r.log.Warn("delivery queue full, device skipped", "device", d.ID)
		}
	}

	return true
}

// deliver pushes one device's share, skipping what it got within the dedupe window.
func (r *Relay) deliver(d delivery) {
	state := r.store.Snapshot()
	device, ok := findDevice(state, d.deviceID)
	if !ok {
		return // removed since the webhook
	}

	alerts := r.dedupe.claim(device.ID, d.alerts)
	if len(alerts) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	status, err := r.sender.send(ctx, state, device, alerts)
	switch {
	case errors.Is(err, errGone):
		r.log.Info("push endpoint gone, device removed", "device", device.ID, "status", status)
		// A fresh deadline: the send may have spent most of its own.
		rmCtx, rmCancel := context.WithTimeout(context.Background(), r.timeout)
		defer rmCancel()
		if err := r.removeDevice(rmCtx, device.ID, device.Endpoint); err != nil {
			r.log.Error("remove device", "device", device.ID, "err", err)
		}
	case err != nil:
		r.dedupe.release(device.ID, alerts)
		// The error may quote the endpoint (a URL error): log the status only.
		r.log.Warn("push failed", "device", device.ID, "status", status)
	default:
		r.log.Info("pushed", "device", device.ID, "status", status, "alerts", len(alerts))
	}
}

// removeDevice removes the device id; with an endpoint, only while it still has that one.
func (r *Relay) removeDevice(ctx context.Context, id, endpoint string) error {
	r.dedupe.forget(id)

	return r.store.Update(ctx, func(s *State) error { return deleteDevice(s, id, endpoint) })
}

// tokenMatches compares in constant time; an empty expected token never matches.
func tokenMatches(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// decodeBody reads a JSON body of at most limit bytes; a non-zero status is the refusal.
func decodeBody(w http.ResponseWriter, req *http.Request, limit int64, out any) (int, string) {
	err := json.NewDecoder(http.MaxBytesReader(w, req.Body, limit)).Decode(out)

	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "body too large"
	case err != nil:
		return http.StatusBadRequest, "invalid JSON body"
	}

	return 0, ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers {error}; msg never quotes a token.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
