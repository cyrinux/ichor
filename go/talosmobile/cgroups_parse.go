package talosmobile

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
)

// The cgroup v2 files a node exposes under /sys/fs/cgroup, read the way `talosctl cgroups`
// does: one Copy (a tar.gz of the whole hierarchy), only the files below parsed. Values
// are kernel units: bytes, microseconds, and PSI percentages.

type cgroupPSI struct {
	Some10 float64 `json:"some10"`
	Some60 float64 `json:"some60"`
	Full10 float64 `json:"full10"`
	Full60 float64 `json:"full60"`
}

type cgroupPressure struct {
	CPU    cgroupPSI `json:"cpu"`
	Memory cgroupPSI `json:"memory"`
	IO     cgroupPSI `json:"io"`
}

// cgroupNode is one cgroup. Limits of 0 mean none (the kernel's "max"); CPU and IO are
// cumulative since boot, the apps turn two samples into rates.
type cgroupNode struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind"` // group, service, pod, container
	MemCurrent    uint64          `json:"memCurrent,omitempty"`
	MemPeak       uint64          `json:"memPeak,omitempty"`
	MemMax        uint64          `json:"memMax,omitempty"`
	MemHigh       uint64          `json:"memHigh,omitempty"`
	MemLow        uint64          `json:"memLow,omitempty"`
	MemMin        uint64          `json:"memMin,omitempty"`
	SwapCurrent   uint64          `json:"swapCurrent,omitempty"`
	OOMKills      uint64          `json:"oomKills,omitempty"`
	CPUUsec       uint64          `json:"cpuUsec,omitempty"`
	ThrottledUsec uint64          `json:"throttledUsec,omitempty"`
	CPUWeight     uint64          `json:"cpuWeight,omitempty"`
	CPULimit      float64         `json:"cpuLimit,omitempty"` // cores, from cpu.max
	IORead        uint64          `json:"ioRead,omitempty"`
	IOWrite       uint64          `json:"ioWrite,omitempty"`
	Pressure      *cgroupPressure `json:"pressure,omitempty"`
	Children      []*cgroupNode   `json:"children,omitempty"`

	children map[string]*cgroupNode
}

// cgroupTreeFromTarGz builds the tree from a Copy of /sys/fs/cgroup.
func cgroupTreeFromTarGz(r io.Reader) (*cgroupNode, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	defer gz.Close() //nolint:errcheck

	root := &cgroupNode{Name: ".", Kind: "group"}
	tr := tar.NewReader(gz)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return root, nil
		}

		if err != nil {
			return nil, err
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		dir, file := path.Split(header.Name)

		// A malformed file is skipped: one odd cgroup must not hide the others.
		_ = root.find(dir).parse(file, tr) //nolint:errcheck
	}
}

func (n *cgroupNode) find(dir string) *cgroupNode {
	node := n

	for part := range strings.SplitSeq(dir, "/") {
		if part == "" || part == "." {
			continue
		}

		if node.children == nil {
			node.children = map[string]*cgroupNode{}
		}

		child, ok := node.children[part]
		if !ok {
			child = &cgroupNode{Name: part, Kind: "group"}
			node.children[part] = child
		}

		node = child
	}

	return node
}

func (n *cgroupNode) parse(file string, r io.Reader) error {
	switch file {
	case "memory.current":
		return parseCgroupValue(r, &n.MemCurrent)
	case "memory.peak":
		return parseCgroupValue(r, &n.MemPeak)
	case "memory.max":
		return parseCgroupValue(r, &n.MemMax)
	case "memory.high":
		return parseCgroupValue(r, &n.MemHigh)
	case "memory.low":
		return parseCgroupValue(r, &n.MemLow)
	case "memory.min":
		return parseCgroupValue(r, &n.MemMin)
	case "memory.swap.current":
		return parseCgroupValue(r, &n.SwapCurrent)
	case "memory.events":
		return parseCgroupFlat(r, map[string]*uint64{"oom_kill": &n.OOMKills})
	case "cpu.stat":
		return parseCgroupFlat(r, map[string]*uint64{"usage_usec": &n.CPUUsec, "throttled_usec": &n.ThrottledUsec})
	case "cpu.weight":
		return parseCgroupValue(r, &n.CPUWeight)
	case "cpu.max":
		return n.parseCPUMax(r)
	case "io.stat":
		return n.parseIOStat(r)
	case "cpu.pressure":
		return parseCgroupPSI(r, &n.pressure().CPU)
	case "memory.pressure":
		return parseCgroupPSI(r, &n.pressure().Memory)
	case "io.pressure":
		return parseCgroupPSI(r, &n.pressure().IO)
	}

	return nil
}

func (n *cgroupNode) pressure() *cgroupPressure {
	if n.Pressure == nil {
		n.Pressure = &cgroupPressure{}
	}

	return n.Pressure
}

// parseCgroupValue reads a single-value file; "max" leaves 0 (no limit).
func parseCgroupValue(r io.Reader, out *uint64) error {
	line, err := firstLine(r)
	if err != nil || line == "max" || line == "" {
		return err
	}

	*out, err = strconv.ParseUint(line, 10, 64)

	return err
}

// parseCgroupFlat reads the wanted keys of a "key value" file.
func parseCgroupFlat(r io.Reader, want map[string]*uint64) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), " ")
		if out, wanted := want[key]; ok && wanted {
			v, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}

			*out = v
		}
	}

	return scanner.Err()
}

// parseCPUMax reads "quota period" (or "max period") as a number of cores.
func (n *cgroupNode) parseCPUMax(r io.Reader) error {
	line, err := firstLine(r)
	if err != nil {
		return err
	}

	quota, period, ok := strings.Cut(line, " ")
	if !ok || quota == "max" {
		return nil
	}

	q, err := strconv.ParseFloat(quota, 64)
	if err != nil {
		return err
	}

	p, err := strconv.ParseFloat(period, 64)
	if err != nil || p == 0 {
		return err
	}

	n.CPULimit = q / p

	return nil
}

// parseIOStat sums rbytes and wbytes over every device ("8:0 rbytes=… wbytes=… …").
func (n *cgroupNode) parseIOStat(r io.Reader) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		_, fields, _ := strings.Cut(scanner.Text(), " ")

		for pair := range strings.FieldsSeq(fields) {
			key, value, _ := strings.Cut(pair, "=")

			v, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				continue
			}

			switch key {
			case "rbytes":
				n.IORead += v
			case "wbytes":
				n.IOWrite += v
			}
		}
	}

	return scanner.Err()
}

// parseCgroupPSI reads "some avg10=… avg60=… avg300=… total=…" and the "full" line.
func parseCgroupPSI(r io.Reader, out *cgroupPSI) error {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		kind, fields, _ := strings.Cut(scanner.Text(), " ")

		var avg10, avg60 float64

		for pair := range strings.FieldsSeq(fields) {
			key, value, _ := strings.Cut(pair, "=")

			switch key {
			case "avg10":
				avg10, _ = strconv.ParseFloat(value, 64) //nolint:errcheck
			case "avg60":
				avg60, _ = strconv.ParseFloat(value, 64) //nolint:errcheck
			}
		}

		switch kind {
		case "some":
			out.Some10, out.Some60 = avg10, avg60
		case "full":
			out.Full10, out.Full60 = avg10, avg60
		}
	}

	return scanner.Err()
}

func firstLine(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return "", scanner.Err()
	}

	return strings.TrimSpace(scanner.Text()), nil
}
