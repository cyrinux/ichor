package ichorgo

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// partsWeb is the site page that shows a part's identity with copy buttons, the vendor's
// support page and a web search. Everything rides after the #: browsers never send it to
// GitHub Pages, and the app itself contacts no vendor.
const partsWeb = "https://cyrinux.github.io/ichor/parts/"

// partsFieldMax caps each field of the link: SMBIOS strings are short, junk is not.
const partsFieldMax = 128

// partsKinds are the parts the page knows how to search for.
var partsKinds = []string{"system", "memory", "disk"}

// partsVendors maps a manufacturer (or a model, for disks reported without one) to the key of
// the page's VENDORS table, first match wins. Adding a vendor is one line here and one there.
var partsVendors = []struct {
	key     string
	needles []string
}{
	{"dell", []string{"dell"}},
	{"lenovo", []string{"lenovo"}},
	{"hpe", []string{"hewlett packard enterprise", "hpe", "hewlett-packard", "hp "}},
	{"supermicro", []string{"super micro", "supermicro"}},
	{"fujitsu", []string{"fujitsu"}},
	{"asus", []string{"asus"}},
	{"intel", []string{"intel"}},
	{"gigabyte", []string{"gigabyte"}},
	{"msi", []string{"micro-star", "msi "}},
	{"raspberrypi", []string{"raspberry pi"}},
	{"seagate", []string{"seagate"}},
	{"wd", []string{"western digital", "wdc"}},
	{"samsung", []string{"samsung"}},
	{"kingston", []string{"kingston"}},
	{"crucial", []string{"crucial", "micron"}},
	{"toshiba", []string{"toshiba", "kioxia"}},
	{"hgst", []string{"hgst", "hitachi"}},
	{"skhynix", []string{"hynix"}},
}

// Disks often report no manufacturer: the model's prefix still names the maker.
var (
	seagateModel = regexp.MustCompile(`^ST\d`)
	wdModel      = regexp.MustCompile(`^(WDC )?WD`)
)

// smbiosPlaceholders are the firmware fillers that mean "not set", compared case-insensitively.
var smbiosPlaceholders = []string{
	"to be filled by o.e.m.", "default string", "system product name", "system manufacturer",
	"system serial number", "not specified", "unknown", "none", "0123456789", "n/a", "oem",
}

// PartsLookupLink is the site page for one part of a node (kind system, memory or disk) with
// its manufacturer, model (the system product, the module's part number or the disk model)
// and serial in the fragment, or "" when there is nothing to look up: an unknown kind, or
// neither a model nor a serial (firmware fillers count as blank). Opened in the browser,
// built from what the hardware screen already shows.
func PartsLookupLink(kind, manufacturer, model, serial string) string {
	if !slices.Contains(partsKinds, kind) {
		return ""
	}
	manufacturer, model, serial = partsField(manufacturer), partsField(model), partsField(serial)
	if model == "" && serial == "" {
		return ""
	}
	q := url.Values{}
	q.Set("v", "1")
	q.Set("k", kind)
	q.Set("vendor", partsVendor(manufacturer, model))
	q.Set("m", manufacturer)
	q.Set("p", model)
	q.Set("s", serial)

	return partsWeb + "#" + q.Encode()
}

// partsField is s trimmed and capped, or "" for a firmware filler.
func partsField(s string) string {
	s = strings.TrimSpace(s)
	if isSMBIOSPlaceholder(s) {
		return ""
	}
	if r := []rune(s); len(r) > partsFieldMax {
		s = strings.TrimSpace(string(r[:partsFieldMax]))
	}

	return s
}

// isSMBIOSPlaceholder reports a value the firmware filled in for want of a real one: blank,
// a known filler, only zeros, or one character repeated.
func isSMBIOSPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	lower := strings.ToLower(s)
	for _, p := range smbiosPlaceholders {
		if lower == p {
			return true
		}
	}
	if strings.Trim(s, "0") == "" {
		return true
	}

	return len(s) > 1 && strings.Count(s, s[:1]) == len(s)
}

// partsVendor is the page's key for the part's maker, or "other".
func partsVendor(manufacturer, model string) string {
	m := " " + strings.ToLower(strings.TrimSpace(manufacturer)) + " "
	for _, v := range partsVendors {
		for _, n := range v.needles {
			if strings.Contains(m, n) || strings.TrimSpace(m) == strings.TrimSpace(n) {
				return v.key
			}
		}
	}
	upper := strings.ToUpper(strings.TrimSpace(model))
	switch {
	case seagateModel.MatchString(upper):
		return "seagate"
	case wdModel.MatchString(upper):
		return "wd"
	}

	return "other"
}
