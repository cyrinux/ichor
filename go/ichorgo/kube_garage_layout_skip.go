package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The escape hatch of a stuck layout migration: `garage layout skip-dead-nodes` from the app.

// KubeGarageSkipDeadNodes lets a draining layout version stop waiting on nodes that are
// down (os:admin): their acknowledgement trackers move to version, the current layout version
// (the report's version). With allowMissingData, every node's sync tracker moves too, so the
// migration ends even if the metadata only the dead nodes held cannot be recovered: that
// metadata is then lost. Like `garage layout skip-dead-nodes`, it is an error when nothing
// moved. Answers the layout report, with the nodes moved in messages.
func KubeGarageSkipDeadNodes(configYAML, contextName, kubeServer, namespace, pod string, version int64, allowMissingData bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-skip-dead-nodes", Namespace: namespace, Object: "Pod/" + pod,
		Params: fmt.Sprintf("version=%d allowMissingData=%t", version, allowMissingData)})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if version <= 0 {
		return "", errors.New("the layout version to skip to must be the current one")
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			return skipGarageDeadNodes(ctx, run, t, version, allowMissingData)
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// skipGarageDeadNodes runs ClusterLayoutSkipDeadNodes and reads the layout back, naming the
// nodes whose trackers moved; nothing moved is an error, worded as Garage's CLI words it.
func skipGarageDeadNodes(ctx context.Context, run execFunc, t garageTarget, version int64, allowMissingData bool) (garageLayoutReport, error) {
	res, err := garageAPI[struct {
		AckUpdated  []string `json:"ackUpdated"`
		SyncUpdated []string `json:"syncUpdated"`
	}](ctx, run, t, "ClusterLayoutSkipDeadNodes", map[string]any{"version": version, "allowMissingData": allowMissingData})
	if err != nil {
		return garageLayoutReport{}, err
	}

	if len(res.AckUpdated) == 0 && len(res.SyncUpdated) == 0 {
		if !allowMissingData {
			return garageLayoutReport{}, errors.New("nothing was done: the remaining nodes cannot complete a metadata sync; allowing missing data forces progress")
		}

		return garageLayoutReport{}, errors.New("nothing to skip: the migration has to run its course")
	}

	report, err := readGarageLayout(ctx, run, t)
	if err != nil {
		return report, err
	}

	names := func(ids []string) string {
		labels := make([]string, 0, len(ids))

		for _, id := range ids {
			label := shortGarageID(id)

			for _, n := range report.Nodes {
				if n.ID == id && n.Hostname != "" {
					label = n.Hostname
				}
			}

			labels = append(labels, label)
		}

		slices.Sort(labels)

		return strings.Join(labels, ", ")
	}

	if len(res.AckUpdated) > 0 {
		report.Messages = append(report.Messages, fmt.Sprintf("Acknowledgement tracker moved to v%d for %s", version, names(res.AckUpdated)))
	}

	if len(res.SyncUpdated) > 0 {
		report.Messages = append(report.Messages, fmt.Sprintf("Sync tracker moved to v%d for %s", version, names(res.SyncUpdated)))
	}

	return report, nil
}
