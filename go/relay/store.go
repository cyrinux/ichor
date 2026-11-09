package relay

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Keys of the relay's Secret.
const (
	keyVAPIDPrivate = "vapidPrivateKey"
	keyVAPIDPublic  = "vapidPublicKey"
	keyPairing      = "pairingToken"
	keyWebhook      = "webhookToken"
	keyDevices      = "devices.json"

	// storeRetries bounds the reload-and-retry loop on a resourceVersion conflict.
	storeRetries = 5
)

// errUnchanged, returned by an Update function, saves nothing and is not an error.
var errUnchanged = errors.New("unchanged")

// State is the relay's state, kept in one Secret.
type State struct {
	VAPIDPrivateKey string
	VAPIDPublicKey  string
	PairingToken    string
	WebhookToken    string
	Devices         []Device
}

// clone copies the state, so a mutation that fails leaves the stored copy untouched.
func (s State) clone() State {
	s.Devices = slices.Clone(s.Devices)

	return s
}

// secret is the part of a core/v1 Secret the relay reads and writes. Data values are base64
// on the wire, which []byte gives for free. Metadata goes back as it was read (labels,
// annotations, owner references and the resourceVersion that makes a PUT conditional).
type secret struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   json.RawMessage   `json:"metadata"`
	Type       string            `json:"type,omitempty"`
	Data       map[string][]byte `json:"data"`
}

// secretMeta is the metadata of a Secret the relay creates.
type secretMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// Store keeps the State in memory and in the Secret namespace/name. Every change is a PUT with
// the last seen resourceVersion; a conflict reloads the Secret and applies the change again.
type Store struct {
	kube      *Kube
	namespace string
	name      string

	// mu serialises the writers; readers take cur without waiting for a write in flight.
	mu  sync.Mutex
	cur atomic.Pointer[State]
	// raw is the last Secret read or written: a PUT keeps its other keys and its metadata.
	raw secret
}

// NewStore returns a store for the Secret namespace/name; call Load before using it.
func NewStore(kube *Kube, namespace, name string) *Store {
	return &Store{kube: kube, namespace: namespace, name: name}
}

func (s *Store) collectionPath() string {
	return "/api/v1/namespaces/" + url.PathEscape(s.namespace) + "/secrets"
}

func (s *Store) path() string { return s.collectionPath() + "/" + url.PathEscape(s.name) }

// Load reads the Secret, creates it when absent and fills in any key it lacks (random
// tokens, a new VAPID key pair), so an admin may pre-create it with only some keys set.
func (s *Store) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for range storeRetries {
		err := s.loadOnce(ctx)
		if !errors.Is(err, errConflict) {
			return err
		}
	}

	return fmt.Errorf("secret %s/%s: still conflicting after %d attempts", s.namespace, s.name, storeRetries)
}

func (s *Store) loadOnce(ctx context.Context) error {
	var sec secret
	err := s.kube.do(ctx, "GET", s.path(), nil, &sec)
	if errors.Is(err, errNotFound) {
		return s.create(ctx)
	}
	if err != nil {
		return fmt.Errorf("read secret %s/%s: %w", s.namespace, s.name, err)
	}

	state, err := decodeState(sec.Data)
	if err != nil {
		return err
	}

	filled, changed, err := fillState(state)
	if err != nil {
		return err
	}

	s.raw = sec
	s.cur.Store(&filled)
	if !changed {
		return nil
	}

	return s.put(ctx, filled)
}

func (s *Store) create(ctx context.Context) error {
	state, _, err := fillState(State{})
	if err != nil {
		return err
	}

	meta, err := json.Marshal(secretMeta{
		Name:      s.name,
		Namespace: s.namespace,
		Labels:    map[string]string{"app.kubernetes.io/name": "ichor-relay"},
	})
	if err != nil {
		return err
	}

	sec := secret{APIVersion: "v1", Kind: "Secret", Metadata: meta, Type: "Opaque"}
	sec.Data, err = encodeState(nil, state)
	if err != nil {
		return err
	}

	var created secret
	if err := s.kube.do(ctx, "POST", s.collectionPath(), sec, &created); err != nil {
		if errors.Is(err, errConflict) {
			return errConflict // created meanwhile by another replica: read it
		}

		return fmt.Errorf("create secret %s/%s: %w", s.namespace, s.name, err)
	}

	s.raw = created
	s.cur.Store(&state)

	return nil
}

// put writes state with the last seen resourceVersion. The caller holds mu.
func (s *Store) put(ctx context.Context, state State) error {
	sec := s.raw
	data, err := encodeState(sec.Data, state)
	if err != nil {
		return err
	}
	sec.Data = data

	var updated secret
	if err := s.kube.do(ctx, "PUT", s.path(), sec, &updated); err != nil {
		if errors.Is(err, errConflict) {
			return errConflict
		}

		return fmt.Errorf("update secret %s/%s: %w", s.namespace, s.name, err)
	}

	s.raw = updated
	s.cur.Store(&state)

	return nil
}

// Snapshot returns a copy of the current state (the zero State before Load).
func (s *Store) Snapshot() State {
	if cur := s.cur.Load(); cur != nil {
		return cur.clone()
	}

	return State{}
}

// Update applies fn to a copy of the state and saves it. On a conflict the Secret is read
// again and fn applied to the fresh state. fn returning an error saves nothing (errUnchanged:
// and Update returns nil).
func (s *Store) Update(ctx context.Context, fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for range storeRetries {
		next := s.Snapshot()
		if err := fn(&next); err != nil {
			if errors.Is(err, errUnchanged) {
				return nil
			}

			return err
		}

		err := s.put(ctx, next)
		if !errors.Is(err, errConflict) {
			return err
		}

		if err := s.loadOnce(ctx); err != nil && !errors.Is(err, errConflict) {
			return err
		}
	}

	return fmt.Errorf("secret %s/%s: still conflicting after %d attempts", s.namespace, s.name, storeRetries)
}

func decodeState(data map[string][]byte) (State, error) {
	state := State{
		VAPIDPrivateKey: string(data[keyVAPIDPrivate]),
		VAPIDPublicKey:  string(data[keyVAPIDPublic]),
		PairingToken:    string(data[keyPairing]),
		WebhookToken:    string(data[keyWebhook]),
	}

	if raw := data[keyDevices]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &state.Devices); err != nil {
			return State{}, fmt.Errorf("%s in the secret: %w", keyDevices, err)
		}
	}

	return state, nil
}

// encodeState returns base with the state's keys set; base's other keys are kept.
func encodeState(base map[string][]byte, state State) (map[string][]byte, error) {
	devices := state.Devices
	if devices == nil {
		devices = []Device{}
	}

	raw, err := json.Marshal(devices)
	if err != nil {
		return nil, err
	}

	data := maps.Clone(base)
	if data == nil {
		data = make(map[string][]byte, 5)
	}
	data[keyVAPIDPrivate] = []byte(state.VAPIDPrivateKey)
	data[keyVAPIDPublic] = []byte(state.VAPIDPublicKey)
	data[keyPairing] = []byte(state.PairingToken)
	data[keyWebhook] = []byte(state.WebhookToken)
	data[keyDevices] = raw

	return data, nil
}

// fillState generates whatever the state lacks; changed says whether it lacked anything.
// Half a VAPID key pair is replaced as a whole.
func fillState(state State) (State, bool, error) {
	changed := false

	if state.VAPIDPrivateKey == "" || state.VAPIDPublicKey == "" {
		private, public, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return State{}, false, fmt.Errorf("generate the VAPID keys: %w", err)
		}
		state.VAPIDPrivateKey, state.VAPIDPublicKey, changed = private, public, true
	}

	for _, token := range []*string{&state.PairingToken, &state.WebhookToken} {
		if *token != "" {
			continue
		}
		t, err := randomToken()
		if err != nil {
			return State{}, false, err
		}
		*token, changed = t, true
	}

	return state, changed, nil
}

// randomToken is 32 random bytes, base64url without padding.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
