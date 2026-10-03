package talosmobile

import "testing"

func TestZoneCountry(t *testing.T) {
	cases := map[string]string{
		// AWS: region and zone
		"eu-west-3": "FR", "eu-west-3a": "FR", "eu-central-1b": "DE", "us-east-1": "US", "us-gov-west-1": "US",
		"ap-northeast-1c": "JP", "sa-east-1a": "BR", "cn-north-1": "CN",
		// GCP: the trailing number is part of the region
		"europe-west1-b": "BE", "europe-west10-a": "DE", "europe-west9": "FR", "us-central1-a": "US",
		// Azure: region and "<region>-<n>" zone
		"francecentral": "FR", "francecentral-1": "FR", "westeurope-2": "NL", "eastus2-1": "US", "uksouth": "GB",
		// Hetzner, OVHcloud, DigitalOcean, Scaleway, Linode
		"fsn1-dc14": "DE", "nbg1": "DE", "hel1-dc2": "FI", "GRA11": "FR", "sbg5": "FR", "BHS5": "CA",
		"UK1": "GB", "DE1": "DE", "ams3": "NL", "nyc1": "US", "fr-par-1": "FR", "nl-ams-1": "NL", "pl-waw-2": "PL",
		"gb-lon": "GB", "se-sto": "SE",
		// home labs
		"home-fr": "FR", "paris-rack-2": "FR", "Germany": "DE", "dc-ch": "CH", "dk-1": "DK", "fr-home-2": "FR",
		// nothing to go by
		"my-lab": "", "at-home": "", "in-house": "", "no-zone": "", "it-rack": "", "sea-rack": "", "lab-de-rack": "",
		"it-mil-1": "IT", "rack-2-us": "US",
		"": "", "zone-a": "", "eu-central": "", "eu-west": "", "rack1": "", "home": "", "az1": "", "default": "",
	}

	for name, want := range cases {
		if got := zoneCountry(name); got != want {
			t.Errorf("zoneCountry(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestZoneCountryFallsBackToRegion(t *testing.T) {
	if got := zoneCountry("zone-b", "eu-west-3"); got != "FR" {
		t.Errorf("got %q, want the region's country", got)
	}

	if got := zoneCountry("fr-par-1", "eu-central-1"); got != "FR" {
		t.Errorf("got %q, want the zone's country first", got)
	}
}
