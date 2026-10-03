package talosmobile

import (
	"sort"
	"strings"
)

// zoneCountry guesses the ISO 3166-1 alpha-2 country of a topology zone or region name
// ("eu-west-3a", "europe-west9-b", "francecentral-1", "fsn1-dc14", "fr-par-1", "home-de"),
// trying each name in turn; "" when none says where it is. The app shows it as a flag.
//
// In order: cloud region names (AWS, GCP, Azure), then per token a city, airport or provider
// site code ("fsn1", "gra11", "ams3", "cdg"), a country name, and last a bare ISO code.
func zoneCountry(names ...string) string {
	for _, name := range names {
		if c := nameCountry(strings.ToLower(strings.TrimSpace(name))); c != "" {
			return c
		}
	}

	return ""
}

func nameCountry(name string) string {
	if name == "" {
		return ""
	}

	for _, region := range cloudRegionKeys {
		if hasRegionPrefix(name, region) {
			return cloudRegions[region]
		}
	}

	tokens := strings.FieldsFunc(name, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })

	for _, t := range tokens {
		if c, ok := siteCodes[strings.TrimRight(t, "0123456789")]; ok {
			return c
		}
	}

	for _, t := range tokens {
		if c, ok := countryNames[t]; ok {
			return c
		}
	}

	// A bare code only at either end, where zone names put it ("fr-par-1", "home-de", "dk-1").
	ends := tokens
	if len(tokens) > 2 {
		ends = []string{tokens[0], tokens[len(tokens)-1]}
	}

	for _, t := range ends {
		if c := isoToken(t); c != "" {
			return c
		}
	}

	return ""
}

func isoToken(t string) string {
	t = strings.TrimRight(t, "0123456789") // "de1", "uk1" (OVH)
	if t == "uk" {
		return "GB"
	}

	if len(t) == 2 && !ambiguousCodes[t] && strings.Contains(isoCountries, " "+t+" ") {
		return strings.ToUpper(t)
	}

	return ""
}

// hasRegionPrefix: name starts with region and does not go on with a digit, so that
// "europe-west1" matches "europe-west1-b" but not "europe-west10-a".
func hasRegionPrefix(name, region string) bool {
	if !strings.HasPrefix(name, region) {
		return false
	}

	rest := name[len(region):]

	return rest == "" || rest[0] < '0' || rest[0] > '9'
}

// cloudRegions: region names that do not spell their country.
var cloudRegions = map[string]string{
	// AWS
	"us-east-1": "US", "us-east-2": "US", "us-west-1": "US", "us-west-2": "US",
	"af-south-1": "ZA", "ap-east-1": "HK", "ap-east-2": "TW", "ap-south-1": "IN", "ap-south-2": "IN",
	"ap-northeast-1": "JP", "ap-northeast-2": "KR", "ap-northeast-3": "JP",
	"ap-southeast-1": "SG", "ap-southeast-2": "AU", "ap-southeast-3": "ID", "ap-southeast-4": "AU",
	"ap-southeast-5": "MY", "ap-southeast-6": "NZ", "ap-southeast-7": "TH",
	"ca-central-1": "CA", "ca-west-1": "CA",
	"eu-central-1": "DE", "eu-central-2": "CH", "eu-west-1": "IE", "eu-west-2": "GB", "eu-west-3": "FR",
	"eu-south-1": "IT", "eu-south-2": "ES", "eu-north-1": "SE",
	"il-central-1": "IL", "me-south-1": "BH", "me-central-1": "AE", "mx-central-1": "MX", "sa-east-1": "BR",
	// GCP
	"northamerica-northeast1": "CA", "northamerica-northeast2": "CA", "northamerica-south1": "MX",
	"southamerica-east1": "BR", "southamerica-west1": "CL",
	"europe-west1": "BE", "europe-west2": "GB", "europe-west3": "DE", "europe-west4": "NL", "europe-west6": "CH",
	"europe-west8": "IT", "europe-west9": "FR", "europe-west10": "DE", "europe-west12": "IT",
	"europe-north1": "FI", "europe-north2": "SE", "europe-central2": "PL", "europe-southwest1": "ES",
	"asia-east1": "TW", "asia-east2": "HK", "asia-northeast1": "JP", "asia-northeast2": "JP", "asia-northeast3": "KR",
	"asia-south1": "IN", "asia-south2": "IN", "asia-southeast1": "SG", "asia-southeast2": "ID",
	"australia-southeast1": "AU", "australia-southeast2": "AU",
	"me-west1": "IL", "me-central1": "QA", "me-central2": "SA", "africa-south1": "ZA",
	// Azure
	"eastus": "US", "eastus2": "US", "westus": "US", "westus2": "US", "westus3": "US", "centralus": "US",
	"northcentralus": "US", "southcentralus": "US", "westcentralus": "US",
	"canadacentral": "CA", "canadaeast": "CA", "brazilsouth": "BR", "brazilsoutheast": "BR", "mexicocentral": "MX",
	"chilecentral": "CL", "northeurope": "IE", "westeurope": "NL", "uksouth": "GB", "ukwest": "GB",
	"francecentral": "FR", "francesouth": "FR", "germanywestcentral": "DE", "germanynorth": "DE",
	"switzerlandnorth": "CH", "switzerlandwest": "CH", "norwayeast": "NO", "norwaywest": "NO",
	"swedencentral": "SE", "polandcentral": "PL", "italynorth": "IT", "spaincentral": "ES",
	"austriaeast": "AT", "belgiumcentral": "BE", "denmarkeast": "DK",
	"eastasia": "HK", "southeastasia": "SG", "japaneast": "JP", "japanwest": "JP",
	"koreacentral": "KR", "koreasouth": "KR", "centralindia": "IN", "southindia": "IN", "westindia": "IN",
	"australiaeast": "AU", "australiasoutheast": "AU", "australiacentral": "AU",
	"uaenorth": "AE", "uaecentral": "AE", "qatarcentral": "QA", "israelcentral": "IL",
	"southafricanorth": "ZA", "southafricawest": "ZA", "indonesiacentral": "ID", "malaysiawest": "MY",
	"newzealandnorth": "NZ", "taiwannorth": "TW",
}

// cloudRegionKeys: the regions longest first, so "eastus2" wins over "eastus".
var cloudRegionKeys = func() []string {
	keys := make([]string, 0, len(cloudRegions))
	for k := range cloudRegions {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}

		return keys[i] < keys[j]
	})

	return keys
}()

// siteCodes: provider site codes (Hetzner, OVH, DigitalOcean, Vultr...), airport codes and
// city names, matched as whole tokens once trailing digits are dropped ("fsn1", "gra11").
var siteCodes = map[string]string{
	// Hetzner
	"fsn": "DE", "nbg": "DE", "hel": "FI", "ash": "US", "hil": "US", "sin": "SG",
	// OVHcloud
	"gra": "FR", "sbg": "FR", "rbx": "FR", "bhs": "CA", "waw": "PL",
	// DigitalOcean, Vultr, Linode, Scaleway and airports
	"par": "FR", "cdg": "FR", "ory": "FR", "mrs": "FR",
	"ams": "NL", "fra": "DE", "muc": "DE", "lon": "GB", "lhr": "GB", "lgw": "GB",
	"bcn": "ES", "mil": "IT", "mxp": "IT", "zrh": "CH", "gva": "CH", "vie": "AT", "bru": "BE",
	"cph": "DK", "osl": "NO", "arn": "SE", "dub": "IE", "lis": "PT", "prg": "CZ",
	"nyc": "US", "jfk": "US", "ewr": "US", "iad": "US", "ord": "US", "dfw": "US", "sfo": "US", "sjc": "US",
	"lax": "US", "atl": "US", "mia": "US", "phx": "US",
	"tor": "CA", "yyz": "CA", "yul": "CA", "yvr": "CA", "yto": "CA",
	"nrt": "JP", "hnd": "JP", "kix": "JP", "tyo": "JP", "osa": "JP", "icn": "KR",
	"sgp": "SG", "hkg": "HK", "syd": "AU", "bom": "IN", "maa": "IN", "blr": "IN",
	"gru": "BR", "sao": "BR", "jnb": "ZA", "dxb": "AE", "tlv": "IL", "scl": "CL", "mex": "MX", "cgk": "ID",
	// cities
	"paris": "FR", "marseille": "FR", "lyon": "FR", "roubaix": "FR", "gravelines": "FR", "strasbourg": "FR",
	"london": "GB", "manchester": "GB", "frankfurt": "DE", "falkenstein": "DE", "nuremberg": "DE",
	"berlin": "DE", "munich": "DE", "amsterdam": "NL", "brussels": "BE", "madrid": "ES", "milan": "IT",
	"zurich": "CH", "geneva": "CH", "vienna": "AT", "warsaw": "PL", "stockholm": "SE", "oslo": "NO",
	"helsinki": "FI", "copenhagen": "DK", "dublin": "IE", "lisbon": "PT", "prague": "CZ",
	"tokyo": "JP", "osaka": "JP", "seoul": "KR", "singapore": "SG", "sydney": "AU", "melbourne": "AU",
	"mumbai": "IN", "toronto": "CA", "montreal": "CA", "vancouver": "CA", "ashburn": "US",
}

var countryNames = map[string]string{
	"france": "FR", "germany": "DE", "netherlands": "NL", "holland": "NL", "belgium": "BE", "spain": "ES",
	"italy": "IT", "switzerland": "CH", "austria": "AT", "poland": "PL", "sweden": "SE", "norway": "NO",
	"finland": "FI", "denmark": "DK", "ireland": "IE", "portugal": "PT", "czechia": "CZ", "ukraine": "UA",
	"britain": "GB", "england": "GB", "scotland": "GB", "japan": "JP", "korea": "KR", "india": "IN",
	"australia": "AU", "canada": "CA", "brazil": "BR", "mexico": "MX", "usa": "US", "luxembourg": "LU",
}

// ambiguousCodes are ISO codes that zone names use for something else: continents in cloud
// regions ("eu-", "ap-", "me-", "af-", "sa-"), "az" for availability zone, and English
// words ("my-lab", "at-home", "in-house", "no-zone").
var ambiguousCodes = map[string]bool{
	"eu": true, "ap": true, "me": true, "af": true, "sa": true, "az": true,
	"my": true, "at": true, "in": true, "is": true, "to": true, "do": true, "be": true, "so": true,
	"no": true, "by": true, "as": true, "am": true, "it": true, "pa": true, "ma": true, "la": true,
}

// isoCountries: the ISO 3166-1 alpha-2 codes, space-separated and padded.
const isoCountries = " ad ae af ag ai al am ao aq ar as at au aw ax az ba bb bd be bf bg bh bi bj bl bm bn bo bq br bs bt bv bw by bz" +
	" ca cc cd cf cg ch ci ck cl cm cn co cr cu cv cw cx cy cz de dj dk dm do dz ec ee eg eh er es et fi fj fk fm fo fr" +
	" ga gb gd ge gf gg gh gi gl gm gn gp gq gr gs gt gu gw gy hk hm hn hr ht hu id ie il im in io iq ir is it je jm jo jp" +
	" ke kg kh ki km kn kp kr kw ky kz la lb lc li lk lr ls lt lu lv ly ma mc md me mf mg mh mk ml mm mn mo mp mq mr ms mt" +
	" mu mv mw mx my mz na nc ne nf ng ni nl no np nr nu nz om pa pe pf pg ph pk pl pm pn pr ps pt pw py qa re ro rs ru rw" +
	" sa sb sc sd se sg sh si sj sk sl sm sn so sr ss st sv sx sy sz tc td tf tg th tj tk tl tm tn to tr tt tv tw tz ua ug" +
	" um us uy uz va vc ve vg vi vn vu wf ws ye yt za zm zw "
