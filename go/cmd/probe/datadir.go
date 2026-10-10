package main

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// probeKeyFile holds the probe's data key in its data directory, as the app keeps its key
// in the Keystore or Keychain.
const probeKeyFile = "probe.key"

// setDataDir gives the core dir to keep its files in, with the key kept there (made on the
// first use).
func setDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path := filepath.Join(dir, probeKeyFile)

	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}

		err = os.WriteFile(path, key, 0o600)
	}

	if err != nil {
		return err
	}

	if len(key) != 32 {
		return errors.New(path + " is not a 32-byte key")
	}

	ichorgo.SetDataDir(dir, key)

	return nil
}
