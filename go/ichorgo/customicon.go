package ichorgo

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
)

// customIconKey lets a resource name its own icon, for software the catalog cannot recognise
// (a personal project, an internal service). An annotation holds any form below; a label, whose
// values are short slugs only, holds a Dashboard Icons slug. The annotation wins.
//
//	ichor.levis.name/icon: grafana                          # bundled, else Dashboard Icons slug
//	ichor.levis.name/icon: https://git.example.org/logo.png # fetched only when downloads are on
//	ichor.levis.name/icon: data:image/png;base64,iVBORw0... # inline, never fetched
const customIconKey = "ichor.levis.name/icon"

// customIconMaxBytes bounds an inline icon once decoded: a tile is 64 dp, a few KB are plenty.
const customIconMaxBytes = 64 * 1024

const customIconMaxURL = 2048

var (
	customIconSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,80}$`)
	// Raster formats both apps decode; SVG is not one (neither BitmapFactory nor ImageIO draw it).
	customIconData = regexp.MustCompile(`^data:image/(png|jpeg|webp|gif);base64,`)
)

// customIconRef is where a custom icon comes from: at most one field is set.
type customIconRef struct {
	icon   string // a bundled catalog icon
	remote string // a Dashboard Icons slug
	url    string // an https URL or a data: URI
}

// customIcon reads the icon a resource names for itself from its annotations, else its labels.
// False when there is none or it is invalid: the caller keeps its catalog guess. An invalid
// annotation does not fall back to the label, so a typo shows as no custom icon at all.
func customIcon(annotations, labels map[string]string) (customIconRef, bool) {
	if v, ok := annotations[customIconKey]; ok {
		return parseCustomIcon(v)
	}

	if v, ok := labels[customIconKey]; ok {
		return parseCustomIcon(v)
	}

	return customIconRef{}, false
}

func parseCustomIcon(v string) (customIconRef, bool) {
	v = strings.TrimSpace(v)

	switch {
	case strings.HasPrefix(v, "data:"):
		return customIconDataURI(v)
	case strings.HasPrefix(strings.ToLower(v), "https://"):
		return customIconURL(v)
	}

	slug := strings.ToLower(v)
	if !customIconSlug.MatchString(slug) {
		return customIconRef{}, false
	}

	if app := loadAppCatalog().byName[slug]; app != nil && app.hasIcon() {
		return customIconRef{icon: app.ID}, true
	}

	return customIconRef{remote: slug}, true
}

// customIconURL accepts an https URL with a host and no credentials, which would be sent along.
func customIconURL(v string) (customIconRef, bool) {
	u, err := url.Parse(v)
	if err != nil || len(v) > customIconMaxURL || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil {
		return customIconRef{}, false
	}

	return customIconRef{url: u.String()}, true
}

// customIconDataURI accepts a base64 raster image up to customIconMaxBytes. Whitespace (a YAML
// folded block) is dropped so the apps get one canonical line.
func customIconDataURI(v string) (customIconRef, bool) {
	header := customIconData.FindString(v)
	if header == "" {
		return customIconRef{}, false
	}

	payload := strings.Join(strings.Fields(v[len(header):]), "")
	if base64.StdEncoding.DecodedLen(len(payload)) > customIconMaxBytes+2 {
		return customIconRef{}, false
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 || len(data) > customIconMaxBytes {
		return customIconRef{}, false
	}

	return customIconRef{url: header + payload}, true
}
