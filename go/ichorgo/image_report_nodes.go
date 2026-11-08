package ichorgo

import (
	"cmp"
	"slices"
)

// nodeUsed is what the containers of one node run: their images and the digests they name.
type nodeUsed struct {
	images  map[string]bool // repo:tag
	digests map[string]bool
}

// reportUnusedImages lists, per node of nodeImages, the CRI images none of the node's containers
// runs (the pod sandbox image aside). One image may be listed under several names (a tag, a
// digest reference): it counts once, under its tagged name when it has one.
func reportUnusedImages(inv inventory, nodeImages map[string][]imageInfo) []reportUnused {
	used := map[string]*nodeUsed{}

	for _, app := range inv.Apps {
		for _, pod := range app.Pods {
			u := used[pod.Node]
			if u == nil {
				u = &nodeUsed{images: map[string]bool{}, digests: map[string]bool{}}
				used[pod.Node] = u
			}

			for _, c := range pod.Containers {
				u.add(reportKeyOf(c.Image))
			}
		}
	}

	out := []reportUnused{}

	for node, images := range nodeImages {
		u := used[node]
		if u == nil {
			u = &nodeUsed{}
		}

		for _, group := range groupNodeImages(images) {
			if image, size, unused := u.unused(group); unused {
				out = append(out, reportUnused{Node: node, Image: image, SizeBytes: size})
			}
		}
	}

	slices.SortFunc(out, func(a, b reportUnused) int { return cmp.Or(cmp.Compare(a.Node, b.Node), cmp.Compare(a.Image, b.Image)) })

	return out
}

func (u *nodeUsed) add(key reportKey) {
	if key.repo != "" {
		u.images[key.repo+":"+key.label()] = true
	}

	if key.digest != "" {
		u.digests[key.digest] = true
	}
}

// unused is whether no container runs the image named by group, with the name to show and its size.
func (u *nodeUsed) unused(group []imageInfo) (name string, size int64, unused bool) {
	tagged := false

	for _, img := range group {
		key := reportKeyOf(img.Name)
		if key.repo != "" && parseImageRef(img.Name).Name() == "pause" {
			return "", 0, false // the pod sandbox: always there, run by no container
		}

		if u.digests[img.Digest] || u.digests[key.digest] || (key.repo != "" && u.images[key.repo+":"+key.label()]) {
			return "", 0, false
		}

		size = max(size, img.Size)

		if name == "" || (key.tag != "" && !tagged) {
			name, tagged = key.display(), key.tag != ""
		}
	}

	return name, size, true
}

// groupNodeImages puts the names of one image (same digest) together, in the order listed.
func groupNodeImages(images []imageInfo) [][]imageInfo {
	index := map[string]int{}

	var groups [][]imageInfo

	for _, img := range images {
		id := cmp.Or(img.Digest, img.Name)

		i, ok := index[id]
		if !ok {
			i = len(groups)
			index[id] = i
			groups = append(groups, nil)
		}

		groups[i] = append(groups[i], img)
	}

	return groups
}
