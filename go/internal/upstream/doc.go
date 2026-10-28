// Package upstream says which newer versions of a container image or a Helm chart exist,
// from the registry's tags list (OCI token auth, Docker Hub's API), a chart repository's
// index.yaml or an OCI chart's tags. It reads no cluster and imports nothing of ichorgo.
//
// The policy, in five lines:
//   - a tag splits into prefix (v, release-), core MAJOR.MINOR[.PATCH], prerelease, suffix;
//   - a candidate counts only with the same prefix, suffix and number of core parts;
//   - prereleases count only when the current version is one;
//   - a tag that is not a version (latest, main, sha-abc123) is reported, not compared;
//   - patch = newest same major.minor, minor = newest same major, major = newest overall.
//
// Answers are cached 6 h per repository or index URL, failures 5 min. Querying a registry
// tells it which images the person runs, from the phone's address: the app asks first.
package upstream
