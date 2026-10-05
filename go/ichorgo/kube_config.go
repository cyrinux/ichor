package ichorgo

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"

	"go.yaml.in/yaml/v4"
)

// kubeCredentials is what the app needs from a kubeconfig to talk to the API server: the
// current context's server, its CA, and a client certificate or bearer token. Exec plugins
// and auth providers cannot run on a phone and are rejected.
type kubeCredentials struct {
	server *url.URL
	tls    *tls.Config
	token  string
	// namespace is the current context's namespace, "" when it sets none.
	namespace string
}

type kubeconfigFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
			TLSServerName            string `yaml:"tls-server-name"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
			Token                 string `yaml:"token"`
			Exec                  any    `yaml:"exec"`
			AuthProvider          any    `yaml:"auth-provider"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			User      string `yaml:"user"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// parseKubeconfig reads the current context of a kubeconfig (the first one when unset).
func parseKubeconfig(data string) (*kubeCredentials, error) {
	var f kubeconfigFile
	if err := yaml.Unmarshal([]byte(data), &f); err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}

	if len(f.Contexts) == 0 {
		return nil, errors.New("kubeconfig has no context")
	}

	ctx := f.Contexts[0].Context
	for _, c := range f.Contexts {
		if c.Name == f.CurrentContext {
			ctx = c.Context
		}
	}

	creds := &kubeCredentials{tls: &tls.Config{MinVersion: tls.VersionTLS12}, namespace: ctx.Namespace}

	found := false

	for _, c := range f.Clusters {
		if c.Name != ctx.Cluster {
			continue
		}

		found = true

		server, err := url.Parse(c.Cluster.Server)
		if err != nil || server.Scheme != "https" || server.Host == "" {
			return nil, fmt.Errorf("kubeconfig server %q is not an https URL", c.Cluster.Server)
		}

		creds.server = server
		creds.tls.ServerName = c.Cluster.TLSServerName
		creds.tls.InsecureSkipVerify = c.Cluster.InsecureSkipTLSVerify //nolint:gosec // the user's own kubeconfig asks for it

		if c.Cluster.CertificateAuthorityData != "" {
			pem, err := base64.StdEncoding.DecodeString(c.Cluster.CertificateAuthorityData)
			if err != nil {
				return nil, fmt.Errorf("kubeconfig CA: %w", err)
			}

			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("kubeconfig CA: no certificate found")
			}

			creds.tls.RootCAs = pool
		}
	}

	if !found {
		return nil, fmt.Errorf("kubeconfig cluster %q not found", ctx.Cluster)
	}

	for _, u := range f.Users {
		if u.Name != ctx.User {
			continue
		}

		if err := creds.setUser(u.User.ClientCertificateData, u.User.ClientKeyData, u.User.Token, u.User.Exec != nil || u.User.AuthProvider != nil); err != nil {
			return nil, err
		}

		return creds, nil
	}

	return nil, fmt.Errorf("kubeconfig user %q not found", ctx.User)
}

func (c *kubeCredentials) setUser(certData, keyData, token string, plugin bool) error {
	switch {
	case certData != "" && keyData != "":
		certPEM, err := base64.StdEncoding.DecodeString(certData)
		if err != nil {
			return fmt.Errorf("kubeconfig client certificate: %w", err)
		}

		keyPEM, err := base64.StdEncoding.DecodeString(keyData)
		if err != nil {
			return fmt.Errorf("kubeconfig client key: %w", err)
		}

		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return fmt.Errorf("kubeconfig client certificate: %w", err)
		}

		c.tls.Certificates = []tls.Certificate{pair}
	case token != "":
		c.token = token
	case plugin:
		return errors.New("kubeconfig uses an exec or auth-provider plugin, which cannot run on a phone")
	default:
		return errors.New("kubeconfig user has no client certificate or token")
	}

	return nil
}
