package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/siderolabs/go-api-signature/pkg/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// omniSigning signs each call of an Omni client with the context's current key, the way
// go-api-signature does (x-sidero-* headers). Not its interceptor: that one reads keys from
// disk and the environment, and opens a browser when Omni refuses them. Here a refusal is a
// sign-in request, on unary calls and streams alike, unless a newer key was stored meanwhile.
type omniSigning struct {
	method string
	// reload reads the stored key again; nil when there is no store (a key being checked).
	reload func() (omniSigner, error)

	mu     sync.Mutex
	signer omniSigner
}

func (o *omniSigning) current() omniSigner {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.signer
}

// expired reports whether the key signs no more: the session must be opened again.
func (o *omniSigning) expired() bool {
	return o.current().key.IsExpired(0)
}

func (o *omniSigning) sign(ctx context.Context, method string) (context.Context, error) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
	} else {
		md = metadata.New(nil)
	}

	signer := o.current()

	msg := message.NewGRPC(md, method)
	if err := msg.Sign(signer.identity, signer.key); err != nil {
		return nil, fmt.Errorf("sign the call: %w", err)
	}

	return metadata.NewOutgoingContext(ctx, msg.Metadata), nil
}

// renew swaps in the stored key when it is another one than the current: false when not.
func (o *omniSigning) renew() bool {
	if o.reload == nil {
		return false
	}

	fresh, err := o.reload()
	if err != nil {
		return false
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if fresh.key.Fingerprint() == o.signer.key.Fingerprint() {
		return false
	}

	o.signer = fresh

	return true
}

// refused turns Omni's refusal of the key into a sign-in request.
func (o *omniSigning) refused(err error) error {
	if status.Code(err) != codes.Unauthenticated {
		return err
	}

	return talosSignInRequired(o.method, "Omni refused the key")
}

func (o *omniSigning) unary() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		call := func() error {
			signed, err := o.sign(ctx, method)
			if err != nil {
				return err
			}

			return invoker(signed, method, req, reply, cc, opts...)
		}

		err := call()
		if status.Code(err) == codes.Unauthenticated && o.renew() {
			err = call()
		}

		return o.refused(err)
	}
}

func (o *omniSigning) stream() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		signed, err := o.sign(ctx, method)
		if err != nil {
			return nil, err
		}

		s, err := streamer(signed, desc, cc, method, opts...)
		if err != nil {
			return nil, o.refused(err)
		}

		return &omniStream{ClientStream: s, signing: o}, nil
	}
}

// omniStream reports a refused key on a stream (grpc-go gives it on the first receive).
type omniStream struct {
	grpc.ClientStream

	signing *omniSigning
}

func (s *omniStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err == nil || errors.Is(err, io.EOF) {
		return err
	}

	return s.signing.refused(err)
}
