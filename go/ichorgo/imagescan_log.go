package ichorgo

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

// imageScanMaxReport bounds one image's Trivy JSON (a few MB for hundreds of findings).
const (
	imageScanMaxReport = 24 << 20
	imageScanMaxLine   = 1 << 20
)

// scanLogParser reads the Trivy pod's log (see imageScanScript) into report as it arrives.
type scanLogParser struct {
	marker  string // "@@<nonce> "
	targets []scanTarget
	report  *imageScanReport
	emit    func(imageScanProgress)

	mode    string // "", "dbfail", "result", "fail": what the lines being read are
	index   int
	buf     bytes.Buffer
	dbError string
	ended   int // images whose end marker was read
}

func newScanLogParser(nonce string, targets []scanTarget, report *imageScanReport, emit func(imageScanProgress)) *scanLogParser {
	return &scanLogParser{marker: "@@" + nonce + " ", targets: targets, report: report, emit: emit}
}

// read consumes a log stream line by line. A line longer than imageScanMaxLine is cut
// (its report then fails to parse, the others are still read).
func (p *scanLogParser) read(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)

	var line []byte

	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) <= imageScanMaxLine {
			line = append(line, chunk...)
		}

		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}

		if len(line) > 0 {
			p.line(strings.TrimSuffix(string(line), "\n"))
		}

		line = line[:0]

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}
	}
}

func (p *scanLogParser) line(line string) {
	rest, ok := strings.CutPrefix(line, p.marker)
	if !ok {
		if p.mode != "" && p.buf.Len()+len(line) < imageScanMaxReport {
			p.buf.WriteString(line)
			p.buf.WriteByte('\n')
		}

		return
	}

	word, arg, _ := strings.Cut(rest, " ")
	i, err := strconv.Atoi(arg)
	valid := err == nil && i >= 0 && i < len(p.targets)
	steps := len(p.targets)

	switch {
	case word == "db":
		p.emit(imageScanProgress{Phase: imageScanPhaseDatabase, Steps: steps})
	case word == "dbfail":
		p.mode = word
		p.buf.Reset()
	case word == "end" && arg == "db":
		p.dbError = trivyError(p.buf.String())
		p.mode = ""
	case word == "scan" && valid:
		p.emit(imageScanProgress{Phase: imageScanPhaseScanning, Step: i + 1, Steps: steps, Image: p.targets[i].image})
	case (word == "result" || word == "fail") && valid:
		p.mode, p.index = word, i
		p.buf.Reset()
	case word == "end" && valid && i == p.index:
		p.finish(i)
	}
}

// finish records image i's report or error from the lines read since its marker.
func (p *scanLogParser) finish(i int) {
	img := p.report.Images[i]
	img.ScannedAt = time.Now().UnixMilli()

	switch p.mode {
	case "result":
		parsed, err := parseTrivyReport(p.buf.Bytes(), img)
		if err != nil {
			img.Error = err.Error()
		} else {
			img = parsed
			img.Error = ""
		}
	case "fail":
		img.Error = trivyError(p.buf.String())
	}

	p.report.Images[i] = img
	p.mode = ""
	p.buf.Reset()
	p.ended++
}

// outcome is the scan's result once the pod ended: the database could not be downloaded,
// the pod stopped before the last image (killed for memory, its deadline), or done.
func (p *scanLogParser) outcome(pod netPerfPod) error {
	if p.dbError != "" {
		return netPerfRefused("Trivy could not download its vulnerability database (a mirror can be set in the scan options): %s", p.dbError)
	}

	if p.ended == len(p.targets) {
		return nil
	}

	reason := pod.Status.Reason
	for _, c := range pod.Status.ContainerStatuses {
		if t := c.State.Terminated; t != nil && reason == "" {
			reason = t.Reason + " (exit code " + strconv.Itoa(t.ExitCode) + ")"
		}
	}

	return netPerfRefused("the Trivy pod stopped after %d of %d images: %s", p.ended, len(p.targets), cmpOr(reason, pod.Status.Phase))
}
