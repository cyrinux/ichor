package talosmobile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

const (
	// readSnaplen is what the readers accept per packet: Talos's afpacket handle captures
	// more than the snap length it writes in the header (talosctl does the same).
	readSnaplen = 262144

	defaultPageSize = 100
	maxPageSize     = 1000
	maxFieldLen     = 300
	maxHexBytes     = 64 << 10
)

type pcapPage struct {
	Packets []packetSummary `json:"packets"`
	Total   int             `json:"total"`
}

type packetDetail struct {
	Layers []detailLayer `json:"layers"`
	Hex    string        `json:"hex"`
}

type detailLayer struct {
	Name   string        `json:"name"`
	Fields []detailField `json:"fields"`
}

type detailField struct {
	K string `json:"k"`
	V string `json:"v"`
}

type packetReader interface {
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
	LinkType() layers.LinkType
}

// openPcap opens a pcap (or pcapng) file.
func openPcap(path string) (packetReader, io.Closer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	br := bufio.NewReaderSize(f, 256<<10)

	magic, err := br.Peek(4)
	if err != nil {
		_ = f.Close() //nolint:errcheck

		return nil, nil, errors.New("not a capture file (too short)")
	}

	var r packetReader

	if bytes.Equal(magic, []byte{0x0a, 0x0d, 0x0d, 0x0a}) {
		r, err = pcapgo.NewNgReader(br, pcapgo.DefaultNgReaderOptions)
	} else {
		var pr *pcapgo.Reader
		if pr, err = pcapgo.NewReader(br); err == nil {
			pr.SetSnaplen(readSnaplen)
			r = pr
		}
	}

	if err != nil {
		_ = f.Close() //nolint:errcheck

		return nil, nil, fmt.Errorf("not a capture file: %w", err)
	}

	return r, f, nil
}

// eachPacket calls fn for every packet of path until it returns false. A truncated last
// packet (a capture still being written) ends the file quietly.
func eachPacket(path string, fn func(i int, ci gopacket.CaptureInfo, data []byte, lt layers.LinkType) bool) error {
	r, closer, err := openPcap(path)
	if err != nil {
		return err
	}

	defer closer.Close() //nolint:errcheck

	lt := r.LinkType()

	for i := 0; ; i++ {
		data, ci, err := r.ReadPacketData()

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil
		case err != nil:
			return fmt.Errorf("packet %d: %w", i, err)
		}

		if !fn(i, ci, data, lt) {
			return nil
		}
	}
}

// ReadPcap lists the packets of a capture file (from StartPacketCapture, or any pcap/pcapng
// file) as {"packets":[summary...],"total":N}: limit summaries (default 100, at most 1000)
// starting at packet offset, and the total packet count.
func ReadPcap(path string, offset, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	offset = max(offset, 0)
	if limit <= 0 {
		limit = defaultPageSize
	}

	limit = min(limit, maxPageSize)

	page := pcapPage{Packets: []packetSummary{}}

	err = eachPacket(path, func(i int, ci gopacket.CaptureInfo, data []byte, lt layers.LinkType) bool {
		page.Total++

		if i >= offset && i < offset+limit {
			page.Packets = append(page.Packets, summarize(i, ci, data, lt))
		}

		return true
	})
	if err != nil {
		return "", err
	}

	return toJSON(page)
}

// PacketDetail decodes packet index (from 0) of a capture file: every protocol layer with
// its fields, and an offset-annotated hex + ASCII dump of the captured bytes.
func PacketDetail(path string, index int) (out string, err error) {
	defer maskResult(&out, &err)

	var (
		detail *packetDetail
		count  int
	)

	err = eachPacket(path, func(i int, ci gopacket.CaptureInfo, data []byte, lt layers.LinkType) bool {
		count++

		if i == index {
			detail = buildDetail(i, ci, data, lt)

			return false
		}

		return true
	})

	switch {
	case err != nil:
		return "", err
	case detail == nil:
		return "", fmt.Errorf("packet %d not found (the capture has %d packets)", index, count)
	}

	return toJSON(detail)
}

func buildDetail(i int, ci gopacket.CaptureInfo, data []byte, lt layers.LinkType) *packetDetail {
	frame := detailLayer{Name: "Frame", Fields: []detailField{
		{"number", fmt.Sprint(i)},
		{"time", ci.Timestamp.UTC().Format("2006-01-02 15:04:05.000000 UTC")},
		{"length", fmt.Sprint(ci.Length)},
		{"captured", fmt.Sprint(len(data))},
		{"link type", lt.String()},
	}}

	out := &packetDetail{Layers: []detailLayer{frame}, Hex: hexDump(data[:min(len(data), maxHexBytes)])}

	p := decodePacket(data, lt)

	for _, l := range p.Layers() {
		out.Layers = append(out.Layers, detailLayer{Name: l.LayerType().String(), Fields: layerFields(l)})
	}

	if s := summarize(i, ci, data, lt); s.Proto == "TLS" || s.Proto == "HTTP" {
		out.Layers = append(out.Layers, detailLayer{Name: s.Proto, Fields: []detailField{{"info", s.Info}}})
	}

	if e := p.ErrorLayer(); e != nil {
		out.Layers = append(out.Layers, detailLayer{Name: "Error", Fields: []detailField{{"decode", e.Error().Error()}}})
	}

	return out
}

// layerFields lists the exported fields of a gopacket layer (minus the raw Contents and
// Payload, which the hex dump shows).
func layerFields(l gopacket.Layer) []detailField {
	v := reflect.ValueOf(l)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return []detailField{}
		}

		v = v.Elem()
	}

	fields := []detailField{}

	if v.Kind() != reflect.Struct {
		return fields
	}

	t := v.Type()

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || f.Anonymous || f.Name == "Contents" || f.Name == "Payload" {
			continue
		}

		fields = append(fields, detailField{K: f.Name, V: truncate(formatValue(v.Field(i), 0), maxFieldLen)})
	}

	return fields
}

var stringerType = reflect.TypeFor[fmt.Stringer]()

// formatValue renders a field: Stringers (IPs, MACs, enums) as such, byte slices as text
// when printable and hex otherwise, slices and structs recursively.
func formatValue(v reflect.Value, depth int) string {
	if !v.IsValid() {
		return ""
	}

	if v.CanInterface() && v.Type().Implements(stringerType) && (v.Kind() != reflect.Pointer || !v.IsNil()) {
		return v.Interface().(fmt.Stringer).String() //nolint:forcetypeassert
	}

	switch v.Kind() { //nolint:exhaustive
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return formatBytes(bytesOf(v))
		}

		if depth >= 3 {
			return fmt.Sprintf("[%d items]", v.Len())
		}

		parts := make([]string, 0, v.Len())
		for i := range min(v.Len(), 16) {
			parts = append(parts, formatValue(v.Index(i), depth+1))
		}

		if v.Len() > 16 {
			parts = append(parts, fmt.Sprintf("+%d", v.Len()-16))
		}

		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Struct:
		if depth >= 3 {
			return "{…}"
		}

		var parts []string

		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.IsExported() && !f.Anonymous && f.Name != "Contents" && f.Name != "Payload" {
				parts = append(parts, f.Name+"="+formatValue(v.Field(i), depth+1))
			}
		}

		return "{" + strings.Join(parts, " ") + "}"
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return ""
		}

		return formatValue(v.Elem(), depth)
	default:
		if v.CanInterface() {
			return fmt.Sprint(v.Interface())
		}

		return ""
	}
}

func bytesOf(v reflect.Value) []byte {
	b := make([]byte, v.Len())
	for i := range b {
		b[i] = byte(v.Index(i).Uint())
	}

	return b
}

func formatBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	printable := true

	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			printable = false

			break
		}
	}

	if printable {
		return string(b)
	}

	const maxShown = 48
	if len(b) > maxShown {
		return fmt.Sprintf("%x… (%d bytes)", b[:maxShown], len(b))
	}

	return fmt.Sprintf("%x", b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n-1] + "…"
}

// hexDump renders data like `hexdump -C`, 16 bytes per line:
// "0000  45 00 00 54 00 00 40 00  40 01 b6 4c 0a 00 00 01  |E..T..@.@..L....|".
func hexDump(data []byte) string {
	var b strings.Builder

	for off := 0; off < len(data); off += 16 {
		line := data[off:min(off+16, len(data))]

		fmt.Fprintf(&b, "%04x  ", off)

		for i := range 16 {
			switch {
			case i < len(line):
				fmt.Fprintf(&b, "%02x ", line[i])
			default:
				b.WriteString("   ")
			}

			if i == 7 {
				b.WriteByte(' ')
			}
		}

		b.WriteString(" |")

		for _, c := range line {
			if c >= 0x20 && c <= 0x7e {
				b.WriteByte(c)
			} else {
				b.WriteByte('.')
			}
		}

		b.WriteString("|\n")
	}

	return b.String()
}
