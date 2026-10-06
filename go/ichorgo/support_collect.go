package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

const (
	// Bounds per log file, so that a chatty service cannot make the bundle huge.
	supportLogLines     = 5000
	supportPodLogLines  = 1000
	supportLogBytes     = 8 << 20
	supportPodNamespace = "kube-system"
	// supportResourceWorkers reads resource types in parallel: a node has about 250.
	supportResourceWorkers = 6
)

func nodeBundleSection(s *session, node string) bundleSection {
	return bundleSection{
		node: node,
		dir:  bundleFileName(node),
		reachable: func(ctx context.Context) error {
			if _, err := s.client.Version(client.WithNode(ctx, node)); err != nil {
				return friendlyErr(err)
			}

			return nil
		},
		steps: []bundleStep{
			{"version", func(ctx context.Context) ([]bundleFile, error) { return collectVersion(ctx, s, node) }},
			{"kernel log", func(ctx context.Context) ([]bundleFile, error) { return collectDmesg(ctx, s, node) }},
			{"service logs", func(ctx context.Context) ([]bundleFile, error) { return collectServiceLogs(ctx, s, node) }},
			{"container logs", func(ctx context.Context) ([]bundleFile, error) { return collectContainerLogs(ctx, s, node) }},
			{"resources", func(ctx context.Context) ([]bundleFile, error) { return collectResources(ctx, s, node) }},
			{"machine config", func(ctx context.Context) ([]bundleFile, error) { return collectMachineConfig(ctx, s, node) }},
			{"mounts", func(ctx context.Context) ([]bundleFile, error) { return collectMounts(ctx, s, node) }},
			{"processes", func(ctx context.Context) ([]bundleFile, error) { return collectProcesses(ctx, s, node) }},
		},
	}
}

func clusterBundleSection(s *session, kube kubeTarget) bundleSection {
	return bundleSection{
		dir: "cluster",
		steps: []bundleStep{
			{"etcd", func(ctx context.Context) ([]bundleFile, error) { return collectEtcd(ctx, s) }},
			{"gitops", func(ctx context.Context) ([]bundleFile, error) { return collectGitOpsFile(ctx, s, kube) }},
		},
	}
}

// collectGitOpsFile writes the Argo CD and Flux state, without repository credentials. Nothing
// when the role has no os:admin or neither runs.
func collectGitOpsFile(ctx context.Context, s *session, kube kubeTarget) ([]bundleFile, error) {
	summary, err := summarizeContext("", s.context)
	if err != nil {
		return nil, err
	}

	g := collectGitOps(ctx, kube, summary.Roles)
	if g == nil {
		return nil, nil
	}

	if g.Note != "" {
		return nil, errors.New(g.Note)
	}

	data, err := json.MarshalIndent(scrubGitOps(*g), "", "  ")
	if err != nil {
		return nil, err
	}

	return []bundleFile{{name: "gitops.json", content: data}}, nil
}

func collectVersion(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	resp, err := s.client.Version(client.WithNode(ctx, node))
	if err != nil {
		return nil, friendlyErr(err)
	}

	v := first(resp.GetMessages())

	text := fmt.Sprintf("tag: %s\nsha: %s\nbuilt: %s\ngo: %s\nos/arch: %s/%s\nplatform: %s (%s)\n",
		v.GetVersion().GetTag(), v.GetVersion().GetSha(), v.GetVersion().GetBuilt(), v.GetVersion().GetGoVersion(),
		v.GetVersion().GetOs(), v.GetVersion().GetArch(), v.GetPlatform().GetName(), v.GetPlatform().GetMode())

	return []bundleFile{{"version.txt", []byte(text)}}, nil
}

func collectDmesg(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	stream, err := s.client.Dmesg(client.WithNode(ctx, node), false, false)
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	b, err := readLogStream(stream.Recv, supportLogBytes)

	return logFile("dmesg.log", b), err
}

func collectServiceLogs(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	nodeCtx := client.WithNode(ctx, node)

	resp, err := s.client.ServiceList(nodeCtx)
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	services := mapServices(first(resp.GetMessages()).GetServices())

	var table bytes.Buffer

	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tSTATE\tHEALTH\tLAST EVENT")

	for _, svc := range services {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", svc.ID, svc.State, svc.Health, svc.LastEvent)
	}

	_ = tw.Flush() //nolint:errcheck

	files := []bundleFile{{"services.txt", table.Bytes()}}

	var failed []string

	for _, svc := range services {
		stream, err := s.client.Logs(nodeCtx, constants.SystemContainerdNamespace, common.ContainerDriver_CONTAINERD, svc.ID, false, supportLogLines)
		if err != nil {
			failed = append(failed, svc.ID+": "+friendlyError(err))

			continue
		}

		b, err := readLogStream(stream.Recv, supportLogBytes)
		if err != nil {
			failed = append(failed, svc.ID+": "+err.Error())
		}

		files = append(files, logFile("service-logs/"+bundleFileName(svc.ID)+".log", b)...)
	}

	return files, joinFailures(failed)
}

// collectContainerLogs lists every Kubernetes container and copies the logs of the
// kube-system ones (control plane, CNI, DNS: what a cluster problem usually needs).
func collectContainerLogs(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	nodeCtx := client.WithNode(ctx, node)

	resp, err := s.client.Containers(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI)
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	containers := mergeContainers(first(resp.GetMessages()).GetContainers(), nil)

	var table bytes.Buffer

	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPOD\tCONTAINER\tSTATUS\tIMAGE")

	for _, c := range containers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.PodNamespace, c.Pod, c.Name, c.Status, c.Image)
	}

	_ = tw.Flush() //nolint:errcheck

	files := []bundleFile{{"containers.txt", table.Bytes()}}

	var failed []string

	for _, c := range containers {
		if c.PodNamespace != supportPodNamespace {
			continue
		}

		stream, err := s.client.Logs(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI, c.ID, false, supportPodLogLines)
		if err != nil {
			failed = append(failed, c.Pod+"/"+c.Name+": "+friendlyError(err))

			continue
		}

		b, err := readLogStream(stream.Recv, supportLogBytes)
		if err != nil {
			failed = append(failed, c.Pod+"/"+c.Name+": "+err.Error())
		}

		files = append(files, logFile("container-logs/"+bundleFileName(c.PodNamespace+"_"+c.Pod+"_"+c.Name)+".log", b)...)
	}

	return files, joinFailures(failed)
}

// collectResources dumps every resource type that is not sensitive as YAML, one file per
// type (default namespace), like `talosctl support`. Sensitive types (secrets, the machine
// config) are never read.
func collectResources(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	defs, err := s.resourceDefinitions(ctx, node)
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		files  []bundleFile
		failed []string
		sem    = make(chan struct{}, supportResourceWorkers)
	)

	for _, rd := range defs {
		spec := rd.TypedSpec()
		if !bundleResource(spec) {
			continue
		}

		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := dumpResources(ctx, s, node, spec)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				failed = append(failed, spec.Type+": "+err.Error())
			case len(content) > 0:
				files = append(files, bundleFile{"resources/" + bundleFileName(spec.Type) + ".yaml", content})
			}
		})
	}

	wg.Wait()

	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	sort.Strings(failed)

	return files, joinFailures(failed)
}

// bundleResource tells whether a resource type goes into the bundle: never a sensitive one.
func bundleResource(spec *meta.ResourceDefinitionSpec) bool {
	return spec.Sensitivity != meta.Sensitive
}

func dumpResources(ctx context.Context, s *session, node string, spec *meta.ResourceDefinitionSpec) ([]byte, error) {
	list, err := s.client.COSI.List(client.WithNode(ctx, node), resource.NewMetadata(spec.DefaultNamespace, spec.Type, "", resource.VersionUndefined))
	if err != nil {
		return nil, friendlyErr(err)
	}

	var out bytes.Buffer

	for i, r := range list.Items {
		text, err := resourceToYAML(r)
		if err != nil {
			return nil, err
		}

		if i > 0 {
			out.WriteString("---\n")
		}

		out.WriteString(text)
	}

	return out.Bytes(), nil
}

func collectMachineConfig(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	// Never with secrets: the bundle is meant to be shared.
	text, err := machineConfigYAML(ctx, s, node, false)
	if err != nil {
		return nil, err
	}

	text, err = scrubMachineConfig(text)
	if err != nil {
		return nil, err
	}

	return []bundleFile{{"machine-config.redacted.yaml", []byte(text)}}, nil
}

func collectMounts(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	resp, err := s.client.Mounts(client.WithNode(ctx, node))
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	var table bytes.Buffer

	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILESYSTEM\tSIZE\tUSED\tAVAILABLE\tUSED%\tMOUNTED ON")

	for _, m := range mapMounts(first(resp.GetMessages()).GetStats()) {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%.1f\t%s\n", m.Filesystem, m.Size, m.Used, m.Available, m.UsedPercent, m.MountedOn)
	}

	_ = tw.Flush() //nolint:errcheck

	return []bundleFile{{"mounts.txt", table.Bytes()}}, nil
}

func collectProcesses(ctx context.Context, s *session, node string) ([]bundleFile, error) {
	resp, err := s.client.Processes(client.WithNode(ctx, node))
	if err != nil {
		return nil, s.friendlyErr(node, err)
	}

	var table bytes.Buffer

	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tPPID\tSTATE\tTHREADS\tCPU-TIME\tRSS\tVMS\tCOMMAND")

	for _, p := range mapProcesses(first(resp.GetMessages()).GetProcesses()) {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%d\t%.2f\t%d\t%d\t%s\n", p.Pid, p.Ppid, p.State, p.Threads, p.CPUTime, p.RSS, p.VMS, p.Args)
	}

	_ = tw.Flush() //nolint:errcheck

	return []bundleFile{{"processes.txt", table.Bytes()}}, nil
}

func collectEtcd(ctx context.Context, s *session) ([]bundleFile, error) {
	overview, err := gatherEtcdOverview(ctx, s)
	if err != nil {
		return nil, err
	}

	b, err := json.MarshalIndent(overview, "", "  ")
	if err != nil {
		return nil, err
	}

	return []bundleFile{{"etcd.json", append(b, '\n')}}, nil
}

// readLogStream reads a Talos byte stream to EOF, keeping at most maxBytes (the end of the
// log). What was read is returned even on error.
func readLogStream(recv func() (*common.Data, error), maxBytes int) ([]byte, error) {
	var buf []byte

	for {
		msg, err := recv()

		switch {
		case errors.Is(err, io.EOF):
			return buf, nil
		case err != nil:
			return buf, friendlyErr(err)
		}

		if e := metaError(msg.GetMetadata()); e != "" {
			return buf, errors.New(e)
		}

		buf = append(buf, msg.GetBytes()...)

		if len(buf) > 2*maxBytes {
			buf = append([]byte{}, buf[len(buf)-maxBytes:]...)
		}
	}
}

func logFile(name string, content []byte) []bundleFile {
	if len(content) == 0 {
		return nil
	}

	if len(content) > supportLogBytes {
		content = content[len(content)-supportLogBytes:]
	}

	return []bundleFile{{name, content}}
}

func joinFailures(failed []string) error {
	if len(failed) == 0 {
		return nil
	}

	return errors.New(strings.Join(failed, "\n"))
}
