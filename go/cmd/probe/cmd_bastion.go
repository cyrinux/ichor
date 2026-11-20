package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// bastionPasswordEnv names the environment variable holding the bastion's password, instead of
// a key file (never a password on the command line).
var bastionPasswordEnv = flag.String("password-env", "", "bastion: read the SSH password from this environment variable instead of a key file")

// bastionScope is a talosconfig context's choice: all (Talos API and Kubernetes) or kube.
var bastionScope = flag.String("bastion-scope", "", "bastion: for a talosconfig context, all (default) or kube (Kubernetes only)")

// SSH bastion: reach a context's API server (and a talosconfig context's Talos API, unless
// -bastion-scope kube) through a jump host. The probe's auth
// store is in memory: the host key is trusted for this run only.
var bastionCommands = []command{
	{name: "bastion", args: "CONTEXT HOST USER [KEYFILE]", run: runBastion},
}

func runBastion(e env) (string, error) {
	contextName, host, user, keyFile := flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4)
	if contextName == "" || host == "" || user == "" || (keyFile == "" && *bastionPasswordEnv == "") {
		return "", errors.New("usage: probe bastion CONTEXT HOST USER KEYFILE (or -password-env NAME instead of KEYFILE)")
	}

	presented, err := ichorgo.KubeBastionHostKey(host)
	if err != nil {
		return "", err
	}

	fmt.Println(presented)

	var key struct {
		HostKey string `json:"hostKey"`
	}
	if err := json.Unmarshal([]byte(presented), &key); err != nil {
		return "", err
	}

	fields := map[string]string{"host": host, "user": user, "hostKey": key.HostKey, "scope": *bastionScope}

	if keyFile != "" {
		pem, err := os.ReadFile(keyFile) //nolint:gosec // the key file the person named
		if err != nil {
			return "", fmt.Errorf("read the key file: %w", err)
		}

		fields["privateKey"] = string(pem)
		fields["passphrase"] = os.Getenv("ICHOR_BASTION_PASSPHRASE")
	} else {
		fields["password"] = os.Getenv(*bastionPasswordEnv)
	}

	raw, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}

	info, err := ichorgo.KubeSetBastion(e.cfg, contextName, string(raw))
	if err != nil {
		return "", err
	}

	fmt.Println(info)

	return ichorgo.KubeNamespaces(e.cfg, contextName, e.kubeServer)
}
