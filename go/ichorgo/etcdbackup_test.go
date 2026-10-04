package ichorgo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSnapshotAtomicWithProgressAndHash(t *testing.T) {
	data := strings.Repeat("etcd", 600_000) // 2.4 MB, several progress steps
	dest := filepath.Join(t.TempDir(), "etcd.snapshot")

	var reports []int64

	size, sum, err := writeSnapshot(strings.NewReader(data), dest, nil, func(n int64) { reports = append(reports, n) })
	if err != nil {
		t.Fatal(err)
	}

	want := sha256.Sum256([]byte(data))
	if size != int64(len(data)) || sum != hex.EncodeToString(want[:]) {
		t.Errorf("size=%d sum=%s", size, sum)
	}

	if got, _ := os.ReadFile(dest); string(got) != data {
		t.Error("content differs")
	}

	if len(reports) < 2 || reports[len(reports)-1] != int64(len(data)) {
		t.Errorf("progress = %v", reports)
	}

	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Error("partial file left behind")
	}
}

type failingReader struct{ n int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n > 0 {
		f.n--

		return len(p), nil
	}

	return 0, errors.New("stream broken")
}

func TestWriteSnapshotFailureLeavesNoFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "etcd.snapshot")

	if _, _, err := writeSnapshot(&failingReader{n: 3}, dest, nil, func(int64) {}); err == nil {
		t.Fatal("expected error")
	}

	for _, p := range []string{dest, dest + ".part"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after failure", p)
		}
	}
}
