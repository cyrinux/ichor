package talosmobile

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
)

func TestTailLinesAcrossChunks(t *testing.T) {
	tl := newTailLines(3)
	for _, chunk := range []string{"one\ntw", "o\nthree\n", "four\nfi", "ve"} {
		tl.write([]byte(chunk))
	}

	got := tl.result()
	if strings.Join(got.Lines, "|") != "three|four|five" || !got.Truncated {
		t.Errorf("got %+v", got)
	}
}

func TestTailLinesNoTruncationAndCRLF(t *testing.T) {
	tl := newTailLines(10)
	tl.write([]byte("a\r\nb\n\n"))

	got := tl.result()
	if strings.Join(got.Lines, "|") != "a|b|" || got.Truncated {
		t.Errorf("got %+v", got)
	}
}

func TestDrainStreamStopsOnEOFAndReportsErrors(t *testing.T) {
	msgs := []*common.Data{{Bytes: []byte("x\n")}, {Bytes: []byte("y\n")}}
	i := 0
	recv := func() (*common.Data, error) {
		if i == len(msgs) {
			return nil, io.EOF
		}
		i++

		return msgs[i-1], nil
	}

	got, err := drainStream(recv, 10)
	if err != nil || strings.Join(got.Lines, "|") != "x|y" {
		t.Fatalf("got %+v err %v", got, err)
	}

	_, err = drainStream(func() (*common.Data, error) { return nil, errors.New("boom") }, 10)
	if err == nil {
		t.Error("expected error")
	}
}

func TestDrainStreamSurfacesNodeError(t *testing.T) {
	recv := func() (*common.Data, error) {
		return &common.Data{Metadata: &common.Metadata{Error: "service not found"}}, nil
	}

	if _, err := drainStream(recv, 10); err == nil || !strings.Contains(err.Error(), "service not found") {
		t.Errorf("err = %v", err)
	}
}

func TestClampTail(t *testing.T) {
	for in, want := range map[int]int{0: defaultLogLines, -5: defaultLogLines, 50: 50, 1_000_000: maxLogLines} {
		if got := clampTail(in); got != want {
			t.Errorf("clampTail(%d) = %d, want %d", in, got, want)
		}
	}
}
