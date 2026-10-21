package ichorgo

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// partsFragment parses the fragment of a PartsLookupLink.
func partsFragment(t *testing.T, link string) url.Values {
	t.Helper()
	page, fragment, ok := strings.Cut(link, "#")
	if !ok || page != partsWeb {
		t.Fatalf("link %q is not on %s", link, partsWeb)
	}
	v, err := url.ParseQuery(fragment)
	if err != nil {
		t.Fatalf("fragment of %q: %v", link, err)
	}

	return v
}

func TestPartsLookupLink(t *testing.T) {
	tests := []struct {
		name                          string
		kind, manufacturer, model, sn string
		wantVendor, wantModel, wantSN string
	}{
		{"dell server with a service tag", "system", "Dell Inc.", "PowerEdge R740", "ABC1234", "dell", "PowerEdge R740", "ABC1234"},
		{"seagate disk without a manufacturer", "disk", "", "ST4000NM0035", "ZC1ABCDE", "seagate", "ST4000NM0035", "ZC1ABCDE"},
		{"wd disk by its model", "disk", "", "WDC WD40EFRX-68N32N0", "WD-WCC7K0ABCDEF", "wd", "WDC WD40EFRX-68N32N0", "WD-WCC7K0ABCDEF"},
		{"memory with a filler serial keeps its part number", "memory", "Samsung", "M393A4K40CB2-CTD", "00000000", "samsung", "M393A4K40CB2-CTD", ""},
		{"micron memory is crucial", "memory", "Micron Technology", "36ASF4G72PZ-2G9E2", "1A2B3C4D", "crucial", "36ASF4G72PZ-2G9E2", "1A2B3C4D"},
		{"supermicro board", "system", "Supermicro", "SYS-5019C-M", "S123456X", "supermicro", "SYS-5019C-M", "S123456X"},
		{"hp server", "system", "HP", "ProLiant DL360 Gen10", "CZJ1234ABC", "hpe", "ProLiant DL360 Gen10", "CZJ1234ABC"},
		{"unknown maker", "system", "Ichor", "Demo server", "DEMO-001", "other", "Demo server", "DEMO-001"},
		{"a serial alone is worth a search", "disk", "", "", "ZC1ABCDE", "other", "", "ZC1ABCDE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := PartsLookupLink(tt.kind, tt.manufacturer, tt.model, tt.sn)
			if link == "" {
				t.Fatal("no link")
			}
			v := partsFragment(t, link)
			if v.Get("v") != "1" || v.Get("k") != tt.kind {
				t.Errorf("v=%q k=%q, want 1 and %q", v.Get("v"), v.Get("k"), tt.kind)
			}
			if v.Get("vendor") != tt.wantVendor {
				t.Errorf("vendor = %q, want %q", v.Get("vendor"), tt.wantVendor)
			}
			if v.Get("p") != tt.wantModel || v.Get("s") != tt.wantSN {
				t.Errorf("p=%q s=%q, want %q and %q", v.Get("p"), v.Get("s"), tt.wantModel, tt.wantSN)
			}
		})
	}
}

func TestPartsLookupLinkNothingToLookUp(t *testing.T) {
	tests := []struct{ name, kind, manufacturer, model, sn string }{
		{"fillers for model and serial", "system", "To be filled by O.E.M.", "Default string", "To Be Filled By O.E.M."},
		{"only zeros and repeats", "system", "System manufacturer", "System Product Name", "0000000"},
		{"blank", "disk", "", "  ", ""},
		{"a cpu has no lookup", "cpu", "Intel", "Xeon E-2236", "ABC1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if link := PartsLookupLink(tt.kind, tt.manufacturer, tt.model, tt.sn); link != "" {
				t.Errorf("link = %q, want none", link)
			}
		})
	}
}

func TestPartsLookupLinkEncodesAndCaps(t *testing.T) {
	link := PartsLookupLink("disk", "", "Some Model", "A&B C="+strings.Repeat("x", 200))
	_, fragment, _ := strings.Cut(link, "#")
	if strings.ContainsAny(fragment, " #?") || strings.Count(fragment, "&") != 5 {
		t.Errorf("fragment not encoded: %q", fragment)
	}
	v := partsFragment(t, link)
	if s := v.Get("s"); !strings.HasPrefix(s, "A&B C=") || len([]rune(s)) != partsFieldMax {
		t.Errorf("serial = %q (%d runes), want it decoded and capped at %d", s, len([]rune(s)), partsFieldMax)
	}
}

func TestSMBIOSPlaceholder(t *testing.T) {
	for _, s := range []string{"", " ", "Default string", "NOT SPECIFIED", "0", "000000", "FFFFFFFF", "0123456789", "N/A"} {
		if !isSMBIOSPlaceholder(s) {
			t.Errorf("%q should be a placeholder", s)
		}
	}
	for _, s := range []string{"ABC1234", "Z1", "10"} {
		if isSMBIOSPlaceholder(s) {
			t.Errorf("%q should not be a placeholder", s)
		}
	}
}

// TestPartsVendorsOnThePage keeps the Go table and the page's VENDORS table in step: every key
// the app can send has an entry the page shows.
func TestPartsVendorsOnThePage(t *testing.T) {
	page, err := os.ReadFile("../../docs/parts/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range partsVendors {
		if !strings.Contains(string(page), `"`+v.key+`":`) {
			t.Errorf("vendor %q is missing from docs/parts/index.html", v.key)
		}
	}
}
