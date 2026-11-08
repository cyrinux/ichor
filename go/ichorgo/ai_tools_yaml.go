package ichorgo

import (
	"errors"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

// object_yaml reads what the object browser reads, then removes more: on the phone a person looks
// at it, here it leaves the phone. The scrubber looks for what is shaped like a secret, and a
// `DB_PASSWORD` next to `value: hunter2` is not.

var (
	secretFlag = regexp.MustCompile(`(?i)^--?[a-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|credential)[a-z0-9_.-]*$`)

	// toolYAMLWords are the plain words of a manifest's API groups and well-known labels: not names.
	toolYAMLWords = []string{
		"apps", "batch", "autoscaling", "name", "instance", "component", "part-of", "managed-by", "version",
		"controller-revision-hash", "pod-template-hash", "metadata.name",
	}
)

func (r *toolRun) objectYAML(args toolArgs) (string, error) {
	kind, namespace, name, err := r.toolObject(args)
	if err != nil {
		return "", err
	}

	t, ref := r.target.kube, toolKinds[kind]

	out, err := KubeObjectYAML(t.config, t.context, t.server, ref.group, ref.version, ref.resource, namespace, name, false)
	if err != nil {
		return "", err
	}

	var object map[string]any
	if err := yaml.Unmarshal([]byte(out), &object); err != nil {
		return "", errors.New("the object could not be read")
	}

	object = hideToolValues(object)

	clean, err := yaml.Marshal(object)
	if err != nil {
		return "", errors.New("the object could not be read")
	}

	return r.clean(string(clean), func(n *diagnosisNames) { n.learnValues(object) }, keepHead), nil
}

// hideToolValues is object without what may hold a credential in plain words: environment
// variables' values, what follows a flag named like a secret, what `kubectl apply` keeps a copy in.
func hideToolValues(object map[string]any) map[string]any {
	out := make(map[string]any, len(object))

	for key, value := range object {
		switch key {
		case "data", "stringData", "binaryData":
			continue
		case "managedFields":
			continue
		case "env":
			out[key] = hideEnv(value)
		case "args", "command":
			out[key] = hideFlagValues(value)
		case "annotations":
			out[key] = withoutLastApplied(value)
		default:
			out[key] = hideNested(value)
		}
	}

	return out
}

func hideNested(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return hideToolValues(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = hideNested(item)
		}

		return out
	default:
		return value
	}
}

func hideEnv(value any) any {
	vars, ok := value.([]any)
	if !ok {
		return hiddenToolValue
	}

	out := make([]any, len(vars))

	for i, item := range vars {
		entry, ok := item.(map[string]any)
		if !ok {
			out[i] = hiddenToolValue

			continue
		}

		kept := make(map[string]any, len(entry))
		for key, v := range entry {
			kept[key] = v
		}

		if _, literal := entry["value"]; literal {
			kept["value"] = hiddenToolValue
		}

		out[i] = kept
	}

	return out
}

func hideFlagValues(value any) any {
	list, ok := value.([]any)
	if !ok {
		return hiddenToolValue
	}

	out := make([]any, len(list))

	for i, item := range list {
		out[i] = item

		if i == 0 {
			continue
		}

		if previous, ok := list[i-1].(string); ok && secretFlag.MatchString(previous) {
			out[i] = hiddenToolValue
		}
	}

	return out
}

func withoutLastApplied(value any) any {
	annotations, ok := value.(map[string]any)
	if !ok {
		return value
	}

	out := make(map[string]any, len(annotations))

	for key, v := range annotations {
		if !strings.HasSuffix(key, "last-applied-configuration") {
			out[key] = v
		}
	}

	return out
}

// learnValues learns every string value of a manifest that is not known to be harmless. In a
// manifest, unlike free text, a plain word is as likely a name as anything: `team: payments`.
func (n *diagnosisNames) learnValues(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			n.learnValues(item)
		}
	case []any:
		for _, item := range v {
			n.learnValues(item)
		}
	case string:
		if v != hiddenToolValue && !strings.ContainsAny(v, " \t\n") && !n.sweepSafe(v, true) {
			n.learn(v, placeholderObj)
		}
	}
}
