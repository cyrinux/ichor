package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	sampleSSOSessionConfig = `# written by aws configure sso
[sso-session my-sso]
sso_start_url = https://example.awsapps.com/start
sso_region = us-east-1
sso_registration_scopes = sso:account:access

[profile dev]
sso_session = my-sso
sso_account_id = 123456789012
sso_role_name = ReadOnly
region = eu-west-1
output = json
`
	sampleLegacyConfig = `[default]
region = us-west-2

[profile legacy]
sso_start_url = https://example.awsapps.com/start
sso_region = eu-central-1
sso_account_id = 210987654321
sso_role_name = Admin
region = eu-central-1
`
	sampleSessionOnlyConfig = `[sso-session corp]
sso_start_url=https://example.awsapps.com/start/
sso_region=ap-southeast-2 ; trailing comment
`
	sampleTwoProfilesConfig = `[profile first]
sso_session = s
sso_account_id = 111111111111
sso_role_name = One
[profile second]
sso_session = s
sso_account_id = 222222222222
sso_role_name = Two
[sso-session s]
sso_start_url = https://example.awsapps.com/start
sso_region = us-east-2
s3 =
  max_concurrent_requests = 20
`
	sampleCredentialsFile = `[default]
aws_access_key_id = AKIAEXAMPLEEXAMPLE00
aws_secret_access_key = invented-secret
`
	sampleCredentialProcessConfig = `[profile tool]
credential_process = /usr/local/bin/sample-helper --profile tool
region = eu-west-1
`
)

func TestParseAWSConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		ok   bool
		want awsConfigImport
	}{
		{"sso-session and profile", sampleSSOSessionConfig, true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start", SSORegion: "us-east-1",
			AccountID: "123456789012", RoleName: "ReadOnly", Region: "eu-west-1", Profile: "dev",
		}},
		{"legacy profile", sampleLegacyConfig, true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start", SSORegion: "eu-central-1",
			AccountID: "210987654321", RoleName: "Admin", Region: "eu-central-1", Profile: "legacy",
		}},
		{"sso-session only", sampleSessionOnlyConfig, true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start/", SSORegion: "ap-southeast-2",
		}},
		{"first profile wins", sampleTwoProfilesConfig, true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start", SSORegion: "us-east-2",
			AccountID: "111111111111", RoleName: "One", Profile: "first",
		}},
		{"profile with a missing session falls back to the session", "[profile a]\nsso_session = nope\n" + sampleSessionOnlyConfig, true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start/", SSORegion: "ap-southeast-2",
		}},
		{"BOM, comment on a header, tab before a comment, colon", "\uFEFF[sso-session s] # work\nsso_start_url: https://example.awsapps.com/start#/\nsso_region = us-east-1\t# main\n", true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start#/", SSORegion: "us-east-1",
		}},
		{"pasted indented", "  [sso-session s]\n  sso_start_url = https://example.awsapps.com/start\n  sso_region = us-east-1\n    nested = ignored\n", true, awsConfigImport{
			StartURL: "https://example.awsapps.com/start", SSORegion: "us-east-1",
		}},
		{"http start URL", "[sso-session s]\nsso_start_url = http://example.awsapps.com/start\nsso_region = us-east-1\n", false, awsConfigImport{}},
		{"no sso region", "[sso-session s]\nsso_start_url = https://example.awsapps.com/start\n", false, awsConfigImport{}},
		{"credential_process", sampleCredentialProcessConfig, false, awsConfigImport{}},
		{"credentials file", sampleCredentialsFile, false, awsConfigImport{}},
		{"kubeconfig", "kind: Config\nclusters: []\ncontexts: []\n", false, awsConfigImport{}},
		{"text before a section", "hello\n[profile a]\nsso_start_url = https://example.awsapps.com/start\nsso_region = us-east-1\n", false, awsConfigImport{}},
		{"empty", "", false, awsConfigImport{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseAWSConfig(tc.text)
			if ok != tc.ok || got != tc.want {
				t.Errorf("parseAWSConfig = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAWSAccessKeysFile(t *testing.T) {
	if !hasAWSAccessKeys(sampleCredentialsFile) {
		t.Error("credentials file not recognised")
	}

	for _, text := range []string{sampleSSOSessionConfig, sampleCredentialProcessConfig, "kind: Config\n", "aws_access_key_id = x"} {
		if hasAWSAccessKeys(text) {
			t.Errorf("%q taken for a credentials file", text)
		}
	}
}

func TestClassifyAWSConfig(t *testing.T) {
	for _, tc := range []struct {
		name, text, kind string
		values           map[string]string
	}{
		{"sso-session and profile", sampleSSOSessionConfig, "credentials", map[string]string{
			awsFieldSSOStartURL: "https://example.awsapps.com/start", awsFieldSSORegion: "us-east-1",
			awsFieldAccountID: "123456789012", awsFieldRoleName: "ReadOnly",
		}},
		{"sso-session only", sampleSessionOnlyConfig, "credentials", map[string]string{
			awsFieldSSOStartURL: "https://example.awsapps.com/start/", awsFieldSSORegion: "ap-southeast-2",
		}},
		{"credentials file", sampleCredentialsFile, "refused", nil},
		{"credentials file pasted indented", "    " + strings.ReplaceAll(strings.TrimSpace(sampleCredentialsFile), "\n", "\n    "), "refused", nil},
		{"credential_process", sampleCredentialProcessConfig, "unknown", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ClassifyImportText(tc.text)
			if err != nil {
				t.Fatal(err)
			}

			var got struct {
				Kind, Provider, Field string
				Values                map[string]string
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got.Kind != tc.kind || len(got.Values) != len(tc.values) {
				t.Fatalf("route = %s, want %s %v", out, tc.kind, tc.values)
			}

			for k, v := range tc.values {
				if got.Values[k] != v {
					t.Errorf("values[%s] = %q, want %q", k, got.Values[k], v)
				}
			}

			if tc.kind == "credentials" && (got.Provider != discoverEKS || got.Field != awsFieldSSOStartURL) {
				t.Errorf("route = %s", out)
			}

			if tc.kind == "refused" && (got.Provider != discoverEKS || got.Field != awsFieldAccessKey) {
				t.Errorf("route = %s", out)
			}

			if _, ok := got.Values[awsFieldRegion]; ok {
				t.Error("awsRegion would pick the access-keys option")
			}

			if strings.Contains(out, "invented-secret") || strings.Contains(out, "AKIAEXAMPLE") {
				t.Fatal("classifier returned credentials")
			}
		})
	}
}
