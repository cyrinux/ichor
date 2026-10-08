package ichorgo

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/cosi-project/runtime/api/v1alpha1"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"go.yaml.in/yaml/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Omni's own API, next to the Talos proxy on the same endpoint: Omni routes the methods of
// its services (omni.*, management.*, oidc.*, auth.*) to itself whatever the headers. The
// few messages ichor needs are encoded by hand rather than importing Omni's client module
// (a newer Go and an alpha Talos machinery).

// omniAuthKey is where the sign-in of ctx's identity on its Omni instance is kept: one
// sign-in serves every cluster of the account, as an Omni key is the identity's.
func omniAuthKey(ctx *clientconfig.Context) string {
	return letterHash(sha256.Sum256([]byte("omni-auth\x00" + omniHost(ctx) + "\x00" + strings.ToLower(omniIdentity(ctx)))))
}

// omniInstanceContext is a context naming only an Omni instance and an identity, before
// any talosconfig of it is stored (adding clusters from an Omni sign-in).
func omniInstanceContext(endpoint, identity string) (*clientconfig.Context, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("no Omni URL")
	}

	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	ctx := &clientconfig.Context{
		Endpoints: []string{strings.TrimSuffix(endpoint, "/")},
		Auth:      clientconfig.Auth{SideroV1: &clientconfig.SideroV1{Identity: strings.TrimSpace(identity)}},
	}

	if omniHost(ctx) == "" {
		return nil, fmt.Errorf("not an Omni URL: %q", endpoint)
	}

	return ctx, nil
}

// omniTestRoots replaces the system roots in tests (nil: the system's, as Omni's are public).
var omniTestRoots *x509.CertPool

// omniDial opens a connection to the Omni API of cfgCtx's instance, its calls signed by
// signing (none: the auth service takes unsigned calls).
func omniDial(cfgCtx *clientconfig.Context, signing *omniSigning) (*grpc.ClientConn, error) {
	host := omniHost(cfgCtx)
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(strings.Trim(host, "[]"), "443")
	}

	tlsConfig, err := omniTLS(cfgCtx)
	if err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))}
	if signing != nil {
		opts = append(opts, grpc.WithChainUnaryInterceptor(signing.unary()), grpc.WithChainStreamInterceptor(signing.stream()))
	}

	return grpc.NewClient("dns:///"+host, opts...)
}

// omniTLS trusts the context's CA (a self-hosted Omni), else the system's roots.
func omniTLS(cfgCtx *clientconfig.Context) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: omniTestRoots}

	if cfgCtx.CA != "" {
		pem, err := base64.StdEncoding.DecodeString(cfgCtx.CA)
		if err != nil {
			return nil, fmt.Errorf("decode the Omni CA: %w", err)
		}

		tlsConfig.RootCAs = x509.NewCertPool()
		if !tlsConfig.RootCAs.AppendCertsFromPEM(pem) {
			return nil, errors.New("the Omni CA holds no certificate")
		}
	}

	return tlsConfig, nil
}

// omniAuthChanged drops what was signed with the sign-in kept under authKey: the sessions,
// the Kubernetes clients and their tokens of every context of that identity.
func omniAuthChanged(authKey string) {
	sessions.forgetAuth(authKey)
	kubeAuth.forgetPrefix(omniKubeSourcePrefix + authKey + "\x00")
	kubeClients.forgetWhere(func(t kubeTarget) bool {
		if isKubeconfig(t.config) {
			return false
		}

		_, ctx, err := resolveContext(t.config, t.context)

		return err == nil && isOmni(ctx) && omniAuthKey(ctx) == authKey
	})
}

// omniInvoke calls method with req, a message encoded by hand, and returns the answer's
// bytes. They ride as the unknown fields of an Empty: the standard proto codec sends them as
// they are, under the plain application/grpc content type Omni routes to its API (a forced
// codec named "proto" makes grpc-go send application/grpc+proto, which Omni's router does
// not take for gRPC and hands to its web server).
func omniInvoke(ctx context.Context, cc *grpc.ClientConn, method string, req []byte) ([]byte, error) {
	in, out := &emptypb.Empty{}, &emptypb.Empty{}
	in.ProtoReflect().SetUnknown(req)

	if err := cc.Invoke(ctx, method, in, out); err != nil {
		return nil, err
	}

	return out.ProtoReflect().GetUnknown(), nil
}

// protoFields returns the length-delimited values of field num in msg, in order.
func protoFields(msg []byte, num protowire.Number) ([][]byte, error) {
	var out [][]byte

	for len(msg) > 0 {
		n, typ, size := protowire.ConsumeTag(msg)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}

		msg = msg[size:]

		if n == num && typ == protowire.BytesType {
			v, size := protowire.ConsumeBytes(msg)
			if size < 0 {
				return nil, protowire.ParseError(size)
			}

			out = append(out, v)
			msg = msg[size:]

			continue
		}

		size = protowire.ConsumeFieldValue(n, typ, msg)
		if size < 0 {
			return nil, protowire.ParseError(size)
		}

		msg = msg[size:]
	}

	return out, nil
}

// protoField is the first value of field num, nil when absent.
func protoField(msg []byte, num protowire.Number) ([]byte, error) {
	values, err := protoFields(msg, num)
	if err != nil || len(values) == 0 {
		return nil, err
	}

	return values[0], nil
}

// omniCluster is a cluster of an Omni account.
type omniCluster struct {
	Name              string `json:"name"`
	TalosVersion      string `json:"talosVersion,omitempty"`
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
}

// omniListClusters lists the clusters the identity may see (Omni's Reader role, or an
// access policy on all clusters: Omni refuses the list otherwise), through Omni's COSI state
// API as omnictl does (Omni's ResourceService is its web UI's, behind its HTTP gateway).
func omniListClusters(ctx context.Context, cc *grpc.ClientConn) ([]omniCluster, error) {
	stream, err := v1alpha1.NewStateClient(cc).List(ctx, &v1alpha1.ListRequest{
		Namespace: "default",
		Type:      "Clusters.omni.sidero.dev",
		Options:   &v1alpha1.ListOptions{},
	})
	if err != nil {
		return nil, err
	}

	var clusters []omniCluster

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return clusters, nil
		}

		if err != nil {
			return nil, err
		}

		res := resp.GetResource()
		if id := res.GetMetadata().GetId(); id != "" {
			talos, kube := omniClusterVersions(res.GetSpec().GetYamlSpec())
			clusters = append(clusters, omniCluster{Name: id, TalosVersion: talos, KubernetesVersion: kube})
		}
	}
}

// omniClusterVersions reads the Talos and Kubernetes versions of a cluster's YAML spec, ""
// when it does not say.
func omniClusterVersions(yamlSpec string) (talos, kube string) {
	var spec map[string]any
	if yaml.Unmarshal([]byte(yamlSpec), &spec) != nil {
		return "", ""
	}

	for key, value := range spec {
		s, ok := value.(string)
		if !ok {
			continue
		}

		switch strings.ReplaceAll(strings.ToLower(key), "_", "") {
		case "talosversion":
			talos = s
		case "kubernetesversion":
			kube = s
		}
	}

	return talos, kube
}

// omniTalosconfig is Omni's talosconfig of cluster for the identity: one context, its
// cluster set (never a break-glass one).
func omniTalosconfig(ctx context.Context, cc *grpc.ClientConn, cluster string) ([]byte, error) {
	return omniConfig(ctx, cc, "/management.ManagementService/Talosconfig", cluster)
}

// omniKubeconfig is Omni's kubeconfig of cluster: its Kubernetes proxy, the identity
// signing in with OIDC (kubelogin's exec, which the app does itself).
func omniKubeconfig(ctx context.Context, cc *grpc.ClientConn, cluster string) ([]byte, error) {
	return omniConfig(ctx, cc, "/management.ManagementService/Kubeconfig", cluster)
}

func omniConfig(ctx context.Context, cc *grpc.ClientConn, method, cluster string) ([]byte, error) {
	ctx = metadata.AppendToOutgoingContext(ctx, "context", cluster)

	resp, err := omniInvoke(ctx, cc, method, nil)
	if err != nil {
		return nil, err
	}

	config, err := protoField(resp, 1)
	if err != nil || len(config) == 0 {
		return nil, fmt.Errorf("omni returned no config for %q", cluster)
	}

	return config, nil
}
