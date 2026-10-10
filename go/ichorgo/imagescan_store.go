package ichorgo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Completed image scans are kept on the phone, in the app's data directory (see SetDataDir),
// encrypted like the audit log: an index of the reports and one file per report, for
// imageScanRetention and at most imageScanMaxSaved reports; without a data directory nothing
// is kept. Each new report is compared with the newest saved scan of the same image digest:
// the findings that were not in it are marked new (markNewFindings), so a second scan says
// "3 new high" instead of listing every finding again.

const (
	imageScanIndexFile = "image-scans.json"
	imageScanDir       = "scans"
	imageScanRetention = 90 * 24 * time.Hour
	imageScanMaxSaved  = 30
)

// savedScan is the index entry of a kept report.
type savedScan struct {
	ID       string      `json:"id"`
	Cluster  string      `json:"cluster"` // the context name, like the audit log
	App      string      `json:"app"`     // the inventory app id, "talos:<node>" for system images
	Scanner  string      `json:"scanner"`
	Started  int64       `json:"started"`
	Finished int64       `json:"finished"`
	Images   int         `json:"images"`
	Summary  vulnSummary `json:"summary"`
	New      vulnSummary `json:"new"`
	// Digests are the images scanned without error, to find a digest's last scan.
	Digests []string `json:"digests,omitempty"`
}

// savedScanIDRe is what an id may be (it names a file): "<started ms>-<8 hex>".
var savedScanIDRe = regexp.MustCompile(`^[0-9]{1,19}-[0-9a-f]{8}$`)

var (
	imageScanStoreMu sync.Mutex
	imageScanNow     = time.Now
)

// ImageScanHistory lists the kept scan reports of cluster ("" for every cluster), newest
// first: [{id,cluster,app,scanner,started,finished,images,summary,new,digests}], new counting
// the findings that were not in the previous scan of the same images.
func ImageScanHistory(cluster string) (out string, err error) {
	defer maskResult(&out, &err)

	cluster = privacy.unmaskContext(strings.TrimSpace(cluster))

	imageScanStoreMu.Lock()
	index := readScanIndexLocked()
	imageScanStoreMu.Unlock()

	shown := make([]savedScan, 0, len(index))

	for i := len(index) - 1; i >= 0; i-- {
		if cluster == "" || index[i].Cluster == cluster {
			shown = append(shown, index[i])
		}
	}

	return toJSON(shown)
}

// ImageScanSaved is a kept report as StartImageScan's OnDone gave it, for the report sheet
// and ImageScanExport.
func ImageScanSaved(id string) (out string, err error) {
	defer maskResult(&out, &err)

	imageScanStoreMu.Lock()
	defer imageScanStoreMu.Unlock()

	data, err := readSavedReportLocked(id)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ImageScanForget deletes one kept report.
func ImageScanForget(id string) (err error) {
	defer maskErr(&err)

	if !savedScanIDRe.MatchString(id) {
		return fmt.Errorf("invalid scan report id %q", id)
	}

	imageScanStoreMu.Lock()
	defer imageScanStoreMu.Unlock()

	return writeScanIndexLocked(slices.DeleteFunc(readScanIndexLocked(), func(s savedScan) bool { return s.ID == id }))
}

// ImageScanClear deletes the kept reports of cluster, or all of them when cluster is "".
func ImageScanClear(cluster string) (err error) {
	defer maskErr(&err)

	cluster = privacy.unmaskContext(strings.TrimSpace(cluster))

	imageScanStoreMu.Lock()
	defer imageScanStoreMu.Unlock()

	return writeScanIndexLocked(slices.DeleteFunc(readScanIndexLocked(), func(s savedScan) bool {
		return cluster == "" || s.Cluster == cluster
	}))
}

// keepScan marks report's new findings against the kept reports, then keeps it when it
// scanned at least one image, and returns it marked. Best effort: a report that cannot be
// written is only not kept.
func keepScan(cluster, app string, report imageScanReport) imageScanReport {
	imageScanStoreMu.Lock()
	defer imageScanStoreMu.Unlock()

	if _, aead := dataStore(); aead == nil {
		return report
	}

	report = markNewFindings(report, previousScanLocked())

	if !slices.ContainsFunc(report.Images, func(img scannedImage) bool { return img.Error == "" }) {
		return report
	}

	_ = saveScanLocked(cluster, app, report)

	return report
}

// markedWithHistory is report with its new findings marked against the kept reports, without
// keeping it (the Trivy Operator's reports stay in the cluster).
func markedWithHistory(report imageScanReport) imageScanReport {
	imageScanStoreMu.Lock()
	defer imageScanStoreMu.Unlock()

	if _, aead := dataStore(); aead == nil {
		return report
	}

	return markNewFindings(report, previousScanLocked())
}

// markNewFindings marks the findings of each image scanned without error that were not in
// previous(digest), the last scan of the same image, and counts them in its NewSummary. An
// image never scanned before has no new findings: a first scan is not "all new".
func markNewFindings(report imageScanReport, previous func(digest string) (scannedImage, bool)) imageScanReport {
	images := make([]scannedImage, len(report.Images))

	for i, img := range report.Images {
		images[i] = img

		if img.Digest == "" || img.Error != "" {
			continue
		}

		prev, ok := previous(img.Digest)
		if !ok {
			continue
		}

		known := map[string]bool{}
		for _, v := range prev.Vulnerabilities {
			known[vulnKey(v)] = true
		}

		vulns := make([]imageVuln, len(img.Vulnerabilities))
		var fresh []imageVuln

		for j, v := range img.Vulnerabilities {
			v.New = !known[vulnKey(v)]
			if v.New {
				fresh = append(fresh, v)
			}

			vulns[j] = v
		}

		images[i].Vulnerabilities = vulns
		images[i].NewSummary = summarizeVulns(fresh)
		images[i].PreviousScannedAt = prev.ScannedAt
	}

	report.Images = images

	return report
}

// vulnKey tells findings apart across scans (the apps use the same key).
func vulnKey(v imageVuln) string {
	return v.ID + "\x00" + v.Package + "\x00" + v.Installed + "\x00" + v.Target
}

// previousScanLocked finds a digest's newest kept scan without error; each kept report is
// read once.
func previousScanLocked() func(digest string) (scannedImage, bool) {
	index := readScanIndexLocked()
	reports := map[string]*imageScanReport{}

	return func(digest string) (scannedImage, bool) {
		for i := len(index) - 1; i >= 0; i-- {
			if !slices.Contains(index[i].Digests, digest) {
				continue
			}

			report, ok := reports[index[i].ID]
			if !ok {
				report = readSavedLocked(index[i].ID)
				reports[index[i].ID] = report
			}

			if report == nil {
				continue
			}

			for _, img := range report.Images {
				if img.Digest == digest && img.Error == "" {
					if img.ScannedAt == 0 {
						img.ScannedAt = report.Finished
					}

					return img, true
				}
			}
		}

		return scannedImage{}, false
	}
}

// saveScanLocked writes report's file, then its index entry.
func saveScanLocked(cluster, app string, report imageScanReport) error {
	dir, aead := dataStore()

	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}

	entry := savedScan{
		ID: fmt.Sprintf("%d-%s", report.Started, hex.EncodeToString(suffix)), Cluster: cluster, App: app,
		Scanner: report.Scanner, Started: report.Started, Finished: report.Finished, Images: len(report.Images),
	}

	for _, img := range report.Images {
		entry.Summary = entry.Summary.plus(img.Summary)
		entry.New = entry.New.plus(img.NewSummary)

		if img.Digest != "" && img.Error == "" {
			entry.Digests = appendNew(entry.Digests, img.Digest)
		}
	}

	data, err := toJSON(report)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(dir, imageScanDir), 0o700); err != nil {
		return err
	}

	if err := writeSealed(savedReportPath(dir, entry.ID), []byte(data), aead); err != nil {
		return err
	}

	return writeScanIndexLocked(append(readScanIndexLocked(), entry))
}

// readScanIndexLocked is the index, oldest first, without the expired entries. A missing
// index or one that does not decrypt (the key was reset) is empty.
func readScanIndexLocked() []savedScan {
	dir, aead := dataStore()
	if aead == nil {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(dir, imageScanIndexFile))
	if err != nil {
		return nil
	}

	plain, err := openSealed(aead, data)
	if err != nil {
		return nil
	}

	var index []savedScan
	if json.Unmarshal(plain, &index) != nil {
		return nil
	}

	cutoff := imageScanNow().Add(-imageScanRetention).UnixMilli()

	return slices.DeleteFunc(index, func(s savedScan) bool { return s.Finished < cutoff })
}

// writeScanIndexLocked keeps the newest imageScanMaxSaved entries of index, then deletes the
// report files no entry names (evicted, expired, forgotten, or left by a reset key).
func writeScanIndexLocked(index []savedScan) error {
	dir, aead := dataStore()
	if aead == nil {
		return nil
	}

	if len(index) > imageScanMaxSaved {
		index = index[len(index)-imageScanMaxSaved:]
	}

	data, err := json.Marshal(index)
	if err != nil {
		return err
	}

	if err := writeSealed(filepath.Join(dir, imageScanIndexFile), data, aead); err != nil {
		return err
	}

	files, _ := os.ReadDir(filepath.Join(dir, imageScanDir))
	for _, f := range files {
		id := strings.TrimSuffix(f.Name(), ".json")
		if !slices.ContainsFunc(index, func(s savedScan) bool { return s.ID == id }) {
			_ = os.Remove(filepath.Join(dir, imageScanDir, f.Name()))
		}
	}

	return nil
}

// readSavedReportLocked is a kept report's JSON, refused for an id not in the index.
func readSavedReportLocked(id string) ([]byte, error) {
	errUnknown := fmt.Errorf("no kept scan report %q", id)

	if !savedScanIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid scan report id %q", id)
	}

	if !slices.ContainsFunc(readScanIndexLocked(), func(s savedScan) bool { return s.ID == id }) {
		return nil, errUnknown
	}

	dir, aead := dataStore()

	data, err := os.ReadFile(savedReportPath(dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errUnknown
	}

	if err != nil {
		return nil, err
	}

	return openSealed(aead, data)
}

// readSavedLocked is a kept report, nil when it cannot be read.
func readSavedLocked(id string) *imageScanReport {
	data, err := readSavedReportLocked(id)
	if err != nil {
		return nil
	}

	var report imageScanReport
	if json.Unmarshal(data, &report) != nil {
		return nil
	}

	return &report
}

func savedReportPath(dir, id string) string {
	return filepath.Join(dir, imageScanDir, id+".json")
}

func (s vulnSummary) plus(o vulnSummary) vulnSummary {
	return vulnSummary{
		Critical: s.Critical + o.Critical, High: s.High + o.High, Medium: s.Medium + o.Medium,
		Low: s.Low + o.Low, Unknown: s.Unknown + o.Unknown, Fixable: s.Fixable + o.Fixable, OS: s.OS + o.OS,
	}
}
