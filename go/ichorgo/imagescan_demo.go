package ichorgo

import (
	"context"
	"time"
)

// imageScanDemoStep is how long each demo scan step takes.
var imageScanDemoStep = 700 * time.Millisecond

// runDemoImageScan plays a scan of two images and returns a canned report.
func runDemoImageScan(ctx context.Context, emit func(imageScanProgress)) (imageScanReport, error) {
	report := newImageScanReport(imageScanSourceScan, "Trivy "+trivyVersion)
	images := demoScannedImages(time.Now().UnixMilli())
	steps := len(images)

	progress := []imageScanProgress{
		{Phase: imageScanPhasePreparing, Steps: steps},
		{Phase: imageScanPhaseStarting, Steps: steps, Message: "ContainerCreating"},
		{Phase: imageScanPhaseDatabase, Steps: steps},
	}
	for i, img := range images {
		progress = append(progress, imageScanProgress{Phase: imageScanPhaseScanning, Step: i + 1, Steps: steps, Image: img.Image})
	}

	progress = append(progress, imageScanProgress{Phase: imageScanPhaseCleaning, Steps: steps})

	for _, p := range progress {
		emit(p)

		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-time.After(imageScanDemoStep):
		}
	}

	report.Images = images
	report.Finished = time.Now().UnixMilli()

	return report, nil
}

func demoOperatorReports() operatorReports {
	return operatorReports{Available: false, Report: newImageScanReport(imageScanSourceOperator, "Trivy Operator")}
}

func demoScannedImages(at int64) []scannedImage {
	web := scannedImage{
		Image: "docker.io/library/nginx:1.25.3", Ref: "docker.io/library/nginx@sha256:4c0fdaa8b6341bfdeca5f18f7837462c80cff90527ee35ef185571e1c327beac",
		Digest: "sha256:4c0fdaa8b6341bfdeca5f18f7837462c80cff90527ee35ef185571e1c327beac", OS: "debian 12.4",
		Pods: []string{"demo/web-7d9c6b5f4-x2k8q", "demo/web-7d9c6b5f4-p9lmn"}, ScannedAt: at,
	}
	api := scannedImage{
		Image: "ghcr.io/example/api:2.4.1", Ref: "ghcr.io/example/api@sha256:9a3b1c2d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9",
		Digest: "sha256:9a3b1c2d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9", OS: "alpine 3.20.3",
		Pods: []string{"demo/api-0"}, ScannedAt: at,
	}

	os := func(id, pkg, installed, fixed, severity, title string, score float64) imageVuln {
		return imageVuln{
			ID: id, Package: pkg, Installed: installed, Fixed: fixed, Severity: severity, Title: title, Score: score,
			URL: "https://avd.aquasec.com/nvd/" + id, Class: vulnClassOS, Type: "debian", Target: "nginx (debian 12.4)",
		}
	}
	lib := func(id, pkg, installed, fixed, severity, title string, score float64) imageVuln {
		return imageVuln{
			ID: id, Package: pkg, Installed: installed, Fixed: fixed, Severity: severity, Title: title, Score: score,
			URL: "https://avd.aquasec.com/nvd/" + id, Class: "lang-pkgs", Type: "gobinary", Target: "usr/local/bin/api",
			PURL: "pkg:golang/" + pkg + "@" + installed,
		}
	}

	web = withVulns(web, []imageVuln{
		os("CVE-2023-0001", "libssl3", "3.0.11-1~deb12u2", "3.0.13-1~deb12u1", "HIGH", "openssl: excessive time spent checking DH keys", 7.5),
		os("CVE-2023-0002", "libexpat1", "2.5.0-1", "2.5.0-1+deb12u1", "CRITICAL", "expat: heap overflow in XML_GetBuffer", 9.8),
		os("CVE-2023-0003", "zlib1g", "1:1.2.13.dfsg-1", "", "CRITICAL", "zlib: integer overflow in MiniZip", 9.8),
		os("CVE-2023-0004", "libc6", "2.36-9+deb12u3", "2.36-9+deb12u4", "MEDIUM", "glibc: off-by-one in getaddrinfo", 5.3),
		os("CVE-2023-0005", "perl-base", "5.36.0-7+deb12u1", "", "LOW", "perl: CPAN.pm does not verify TLS certificates", 3.7),
	})
	api = withVulns(api, []imageVuln{
		lib("CVE-2023-0006", "golang.org/x/net", "v0.17.0", "0.23.0", "MEDIUM", "golang: net/http: unlimited CONTINUATION frames", 5.3),
		lib("CVE-2023-0007", "stdlib", "v1.21.3", "1.21.11, 1.22.4", "HIGH", "golang: net/netip: unexpected behavior from Is methods", 7.5),
	})

	return []scannedImage{web, api}
}
