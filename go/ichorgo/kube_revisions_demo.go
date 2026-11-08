package ichorgo

import (
	"strconv"
	"strings"
	"time"
)

// demoWorkloadRevisions is a workload of the demo with three kept revisions, the newest
// current: its own images, then older tags of them.
func demoWorkloadRevisions(kind, namespace, name string) workloadRevisions {
	out := workloadRevisions{Kind: kind, Revisions: []deploymentRevision{}}

	for _, r := range demoRevisionEntries(kind, namespace, name) {
		out.Revisions = append(out.Revisions, r.revision)
	}

	return out
}

func demoRevisionEntries(kind, namespace, name string) []revisionEntry {
	images := []string{"ghcr.io/example/" + name + ":2.4.1"}
	replicas := int32(0)

	for _, w := range demoKubeWorkloads() {
		if w.Kind == kind && w.Namespace == namespace && w.Name == name && len(w.Images) > 0 {
			images, replicas = w.Images, w.Desired
		}
	}

	now := time.Now()
	ages := []time.Duration{2 * time.Hour, 50 * time.Hour, 9 * 24 * time.Hour}
	causes := []string{"", "bump to " + imageTag(olderImage(images[0], 1)), ""}
	hashes := []string{"7d9c6", "5b8f4", "6c2a1"}
	entries := make([]revisionEntry, 0, len(ages))

	for i, age := range ages {
		revImages := make([]string, len(images))
		for j, image := range images {
			revImages[j] = olderImage(image, i)
		}

		rev := deploymentRevision{
			Revision: 7 - i, ReplicaSet: name + "-" + hashes[i], Created: now.Add(-age).UnixMilli(),
			Images: revImages, ChangeCause: causes[i], Current: i == 0,
		}
		if i == 0 && kind == "Deployment" {
			rev.Replicas = replicas
		}

		entries = append(entries, revisionEntry{revision: rev, template: demoPodTemplate(name, revImages, i == 2)})
	}

	return entries
}

// demoPodTemplate is a small pod template; the oldest revision still logs in debug.
func demoPodTemplate(name string, images []string, debug bool) map[string]any {
	containers := make([]any, 0, len(images))

	for i, image := range images {
		c := map[string]any{
			"name": name, "image": image, "imagePullPolicy": "IfNotPresent",
			"resources": map[string]any{"requests": map[string]any{"cpu": "100m", "memory": "128Mi"}},
		}
		if i > 0 {
			c["name"] = name + "-" + strconv.Itoa(i)
		}

		if debug {
			c["env"] = []any{map[string]any{"name": "LOG_LEVEL", "value": "debug"}}
		}

		containers = append(containers, c)
	}

	return map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app": name}},
		"spec":     map[string]any{"containers": containers, "terminationGracePeriodSeconds": 30},
	}
}

// demoRevisionDiff is KubeRevisionDiff in the demo.
func demoRevisionDiff(kind, namespace, name string, revision int) (revisionDiff, error) {
	entries := demoRevisionEntries(kind, namespace, name)

	for _, e := range entries {
		if e.revision.Revision == revision {
			return buildRevisionDiff(entries[0].revision.Revision, entries[0].template, e)
		}
	}

	return revisionDiff{}, errRevisionGone(revision, name)
}

// olderImage is image with a tag steps versions older: the rightmost number of the tag that
// can go down by steps does ("nginx:1.27" → "nginx:1.25" for 2). Unchanged without a tag.
func olderImage(image string, steps int) string {
	tag := imageTag(image)
	if tag == "" || steps == 0 || strings.Contains(image, "@") {
		return image
	}

	prefix := ""
	if strings.HasPrefix(tag, "v") {
		prefix, tag = "v", tag[1:]
	}

	parts := strings.Split(tag, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		if n, err := strconv.Atoi(parts[i]); err == nil && n >= steps {
			parts[i] = strconv.Itoa(n - steps)

			return image[:len(image)-len(imageTag(image))] + prefix + strings.Join(parts, ".")
		}
	}

	return image
}
