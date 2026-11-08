package flux

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// A source's artifact is the tarball source-controller built from Git, an OCI image or a
// bucket, the one kustomize-controller builds from. It is read by exec in the source-controller
// pod (cat of its storage file): the API server's service proxy is the fallback only, because
// the NetworkPolicy `flux install` adds lets in nothing from outside flux-system, the API
// server included once it runs on another node.

const (
	artifactMaxBytes = 64 << 20 // the tarball
	// ArtifactMaxUnpacked bounds every file in it, kept in memory on the phone.
	ArtifactMaxUnpacked = 128 << 20
	// ArtifactMaxFiles bounds how many files it holds.
	ArtifactMaxFiles    = 20000
	artifactExecTimeout = 30 * time.Second
	artifactProxyWait   = 15 * time.Second
	storageDefault      = "/data"
)

var (
	// artifactPathPattern is what a storage path looks like ("gitrepository/ns/name/rev.tar.gz"):
	// nothing a shell or a path traversal could use.
	artifactPathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:/-]*$`)
	ErrNoArtifact       = errors.New("the source has no artifact yet: reconcile it first")
)

// Artifact is a source's status.artifact.
type Artifact struct {
	Path     string `json:"path"`
	URL      string `json:"url"`
	Revision string `json:"revision"`
}

// artifactServer is the source-controller Service an artifact URL points at.
type artifactServer struct {
	namespace, service, port string
}

func parseArtifactURL(raw string) (artifactServer, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return artifactServer{}, fmt.Errorf("unexpected artifact URL %q", raw)
	}

	labels := strings.Split(strings.TrimSuffix(u.Hostname(), "."), ".")
	if len(labels) < 2 {
		return artifactServer{}, fmt.Errorf("unexpected artifact host %q", u.Hostname())
	}

	return artifactServer{namespace: labels[1], service: labels[0], port: cmp.Or(u.Port(), "80")}, nil
}

// FetchArtifact reads the tarball of art, by exec in the controller pod, else through the
// API server's service proxy.
func FetchArtifact(ctx context.Context, k Env, art *Artifact) ([]byte, error) {
	if art == nil || art.Path == "" {
		return nil, ErrNoArtifact
	}

	p := strings.TrimPrefix(path.Clean("/"+art.Path), "/")
	if !artifactPathPattern.MatchString(p) || strings.Contains(p, "..") {
		return nil, fmt.Errorf("unexpected artifact path %q", art.Path)
	}

	srv, err := parseArtifactURL(art.URL)
	if err != nil {
		return nil, err
	}

	data, execErr := fetchArtifactExec(ctx, k, srv, p)
	if execErr == nil {
		return data, nil
	}

	data, proxyErr := fetchArtifactProxy(ctx, k, srv, p)
	if proxyErr == nil {
		return data, nil
	}

	return nil, fmt.Errorf("could not read the source's artifact: in the source-controller pod: %w; through the API server: %w", execErr, proxyErr)
}

type controllerPod struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Containers []struct {
			Name string   `json:"name"`
			Args []string `json:"args"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func fetchArtifactExec(ctx context.Context, k Env, srv artifactServer, p string) ([]byte, error) {
	if k.Exec == nil {
		return nil, errors.New("exec is not available")
	}

	var svc struct {
		Spec struct {
			Selector map[string]string `json:"selector"`
		} `json:"spec"`
	}

	if err := k.Get(ctx, "/api/v1/namespaces/"+url.PathEscape(srv.namespace)+"/services/"+url.PathEscape(srv.service), &svc); err != nil {
		return nil, err
	}

	if len(svc.Spec.Selector) == 0 {
		return nil, errors.New("the source-controller Service selects no pods")
	}

	var sel []string
	for key, v := range svc.Spec.Selector {
		sel = append(sel, key+"="+v)
	}

	var pods kube.List[controllerPod]
	if err := kube.GetList(ctx, k.Client, "/api/v1/namespaces/"+url.PathEscape(srv.namespace)+"/pods?labelSelector="+url.QueryEscape(strings.Join(sel, ",")), &pods); err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != "Running" || len(pod.Spec.Containers) == 0 {
			continue
		}

		c := pod.Spec.Containers[0]
		for _, x := range pod.Spec.Containers {
			if x.Name == "manager" {
				c = x
			}
		}

		file := path.Join(storagePath(c.Args), p)

		out, stderr, err := k.Exec(ctx, artifactExecTimeout, artifactMaxBytes, srv.namespace, pod.Metadata.Name, c.Name, []string{"cat", file})
		if err != nil {
			if len(stderr) > 0 {
				return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
			}

			return nil, err
		}

		return out, nil
	}

	return nil, errors.New("no source-controller pod is running")
}

// storagePath is the --storage-path the controller runs with.
func storagePath(args []string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--storage-path="); ok {
			return path.Clean(v)
		}

		if a == "--storage-path" && i+1 < len(args) {
			return path.Clean(args[i+1])
		}
	}

	return storageDefault
}

func fetchArtifactProxy(ctx context.Context, k Env, srv artifactServer, p string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, artifactProxyWait)
	defer cancel()

	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}

	proxy := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%s/proxy/%s",
		url.PathEscape(srv.namespace), url.PathEscape(srv.service), url.PathEscape(srv.port), strings.Join(segments, "/"))

	status, _, body, err := k.GetRaw(ctx, proxy, nil)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("no answer (a NetworkPolicy in the Flux namespace may block the API server)")
		}

		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("source-controller answered %d", status)
	}

	return body, nil
}

// UntarArtifact unpacks a tarball under root through write, regular files only, every path
// kept below root.
func UntarArtifact(data []byte, write func(path string, content []byte) error, root string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}

	tr := tar.NewReader(gz)
	total, files := int64(0), 0

	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read artifact: %w", err)
		}

		if h.Typeflag != tar.TypeReg {
			continue
		}

		files++
		total += h.Size

		if files > ArtifactMaxFiles || total > ArtifactMaxUnpacked {
			return fmt.Errorf("the artifact is larger than the app unpacks (%d files, %d MiB)", ArtifactMaxFiles, ArtifactMaxUnpacked>>20)
		}

		b, err := io.ReadAll(io.LimitReader(tr, h.Size))
		if err != nil {
			return fmt.Errorf("read artifact: %w", err)
		}

		if err := write(path.Join(root, path.Clean("/"+h.Name)), b); err != nil {
			return err
		}
	}
}
