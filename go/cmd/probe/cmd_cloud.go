package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The token comes from HETZNER_TOKEN, the OVHcloud service account from OVH_ENDPOINT, OVH_CLIENT_ID,
// OVH_CLIENT_SECRET and OVH_PROJECT, the AWS keys from AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY,
// AWS_SESSION_TOKEN and AWS_REGION, the Azure service principal from AZURE_TENANT_ID,
// AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_SUBSCRIPTION_ID and AZURE_LOCATION, the Google
// service account key from GOOGLE_APPLICATION_CREDENTIALS (the key file's path) or
// GCP_SERVICE_ACCOUNT_JSON, with GCP_PROJECT and GCP_ZONE (never a flag: it would show in the
// shell history). The Scaleway key comes from SCW_ACCESS_KEY, SCW_SECRET_KEY,
// SCW_DEFAULT_PROJECT_ID and SCW_DEFAULT_ZONE, the names Scaleway's own CLI reads.
var cloudCommands = []command{
	{name: "hetzner", args: "test|options|estimate ARCH LOCATION|snapshot ARCH LOCATION -yes|create TEMPLATE ROLE HOSTNAME DISK TYPE LOCATION SNAPSHOT_ID -yes", run: func(e env) (string, error) {
		token := os.Getenv("HETZNER_TOKEN")
		if token == "" && flag.Arg(1) != "options" {
			return "", errors.New("set HETZNER_TOKEN to a scratch project's token")
		}

		tokenJSON, err := json.Marshal(map[string]string{"token": token})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.HetznerTest(string(tokenJSON))
		case "options":
			return ichorgo.HetznerOptions(e.cfg, e.context, string(tokenJSON))
		case "create":
			return hetznerCreate(e, string(tokenJSON))
		case "estimate":
			return ichorgo.HetznerSnapshotEstimate(string(tokenJSON), flag.Arg(2), flag.Arg(3))
		case "snapshot":
			return hetznerSnapshot(e, string(tokenJSON))
		}

		return "", errors.New("usage: hetzner test|options|estimate|snapshot|create …")
	}},
	{name: "ovh", args: "test|options|policy|create TEMPLATE ROLE HOSTNAME DISK REGION FLAVOR_ID IMAGE_ID -yes", run: func(e env) (string, error) {
		credJSON, err := json.Marshal(map[string]string{
			"endpoint": os.Getenv("OVH_ENDPOINT"), "clientId": os.Getenv("OVH_CLIENT_ID"),
			"clientSecret": os.Getenv("OVH_CLIENT_SECRET"), "project": os.Getenv("OVH_PROJECT"),
		})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.OVHTest(string(credJSON))
		case "options":
			return ichorgo.OVHOptions(e.cfg, e.context, string(credJSON))
		case "policy":
			return ichorgo.OVHServiceAccountPolicy(os.Getenv("OVH_PROJECT")), nil
		case "create":
			return ovhCreate(e, string(credJSON))
		}

		return "", errors.New("usage: ovh test|options|policy|create …")
	}},
	{name: "scaleway", args: "test|options|policy|create TEMPLATE ROLE HOSTNAME DISK ZONE TYPE IMAGE_ID -yes", run: func(e env) (string, error) {
		credJSON, err := json.Marshal(map[string]string{
			"accessKey": os.Getenv("SCW_ACCESS_KEY"), "secretKey": os.Getenv("SCW_SECRET_KEY"),
			"project": os.Getenv("SCW_DEFAULT_PROJECT_ID"), "zone": os.Getenv("SCW_DEFAULT_ZONE"),
		})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.ScalewayTest(string(credJSON))
		case "options":
			return ichorgo.ScalewayOptions(e.cfg, e.context, string(credJSON))
		case "policy":
			return ichorgo.ScalewayPolicy(os.Getenv("SCW_DEFAULT_PROJECT_ID")), nil
		case "create":
			return scalewayCreate(e, string(credJSON))
		}

		return "", errors.New("usage: scaleway test|options|policy|create …")
	}},
	{name: "aws", args: "test|options|policy|price TYPE|estimate ARCH REGION|image TEMPLATE ARCH SUBNET SG|- -yes|create TEMPLATE ROLE HOSTNAME DISK TYPE AMI SUBNET SG[,SG] -yes|" +
		"signin START_URL SSO_REGION [ACCOUNT ROLE]", run: func(e env) (string, error) {
		credJSON, err := json.Marshal(map[string]string{
			"accessKeyId": os.Getenv("AWS_ACCESS_KEY_ID"), "secretAccessKey": os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"sessionToken": os.Getenv("AWS_SESSION_TOKEN"), "region": os.Getenv("AWS_REGION"),
		})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.AWSTest(string(credJSON))
		case "options":
			return ichorgo.AWSOptions(e.cfg, e.context, string(credJSON))
		case "policy":
			return ichorgo.AWSIAMPolicy(), nil
		case "price":
			return ichorgo.AWSPrice(string(credJSON), flag.Arg(2))
		case "estimate":
			return ichorgo.AWSImageEstimate(string(credJSON), flag.Arg(2), flag.Arg(3))
		case "image":
			return awsImage(e, string(credJSON))
		case "create":
			return awsCreate(e, string(credJSON))
		case "signin":
			return awsSignIn()
		}

		return "", errors.New("usage: aws test|options|policy|price TYPE|estimate|image|create|signin …")
	}},
	{name: "azure", args: "test|options|role [RG]|price LOCATION SIZE|estimate ARCH LOCATION|image TEMPLATE ARCH RG LOCATION SUBNET_ID NSG_ID|- -yes|" +
		"create TEMPLATE ROLE HOSTNAME DISK RG LOCATION SIZE IMAGE_ID SUBNET_ID NSG_ID|- -yes", run: func(e env) (string, error) {
		credJSON, err := json.Marshal(map[string]string{
			"tenantId": os.Getenv("AZURE_TENANT_ID"), "clientId": os.Getenv("AZURE_CLIENT_ID"), "clientSecret": os.Getenv("AZURE_CLIENT_SECRET"),
			"subscriptionId": os.Getenv("AZURE_SUBSCRIPTION_ID"), "location": os.Getenv("AZURE_LOCATION"),
		})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.AzureTest(string(credJSON))
		case "options":
			return ichorgo.AzureOptions(e.cfg, e.context, string(credJSON))
		case "role":
			return ichorgo.AzureRoleDefinition(os.Getenv("AZURE_SUBSCRIPTION_ID"), flag.Arg(2)), nil
		case "price":
			return ichorgo.AzurePrice(string(credJSON), flag.Arg(2), flag.Arg(3))
		case "estimate":
			return ichorgo.AzureImageEstimate(string(credJSON), flag.Arg(2), flag.Arg(3))
		case "image":
			return azureImage(e, string(credJSON))
		case "create":
			return azureCreate(e, string(credJSON))
		}

		return "", errors.New("usage: azure test|options|role [RG]|price LOCATION SIZE|estimate|image|create …")
	}},
	{name: "gcp", args: "test|options|role|price ZONE TYPE|estimate ARCH ZONE|image TEMPLATE ARCH ZONE SUBNET -yes|" +
		"create TEMPLATE ROLE HOSTNAME DISK PROJECT ZONE TYPE IMAGE SUBNET -yes", run: func(e env) (string, error) {
		if flag.Arg(1) == "role" {
			return ichorgo.GCPRoleDefinition(os.Getenv("GCP_PROJECT")), nil
		}

		credJSON, err := gcpCredential()
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.GCPTest(credJSON)
		case "options":
			return ichorgo.GCPOptions(e.cfg, e.context, credJSON)
		case "price":
			return ichorgo.GCPPrice(credJSON, flag.Arg(2), flag.Arg(3))
		case "estimate":
			return ichorgo.GCPImageEstimate(credJSON, flag.Arg(2), flag.Arg(3))
		case "image":
			return gcpImage(e, credJSON)
		case "create":
			return gcpCreate(e, credJSON)
		}

		return "", errors.New("usage: gcp test|options|role|price ZONE TYPE|estimate|image|create …")
	}},
}

// gcpCredential is the GCP credential JSON: the key file GOOGLE_APPLICATION_CREDENTIALS names,
// else GCP_SERVICE_ACCOUNT_JSON's text.
func gcpCredential() (string, error) {
	key := os.Getenv("GCP_SERVICE_ACCOUNT_JSON")

	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // the person's own key file
		if err != nil {
			return "", errors.New("GOOGLE_APPLICATION_CREDENTIALS names no readable key file")
		}

		key = string(data)
	}

	if key == "" {
		return "", errors.New("set GOOGLE_APPLICATION_CREDENTIALS to a scratch project's service account key file")
	}

	credJSON, err := json.Marshal(map[string]string{"serviceAccountJson": key, "project": os.Getenv("GCP_PROJECT"), "zone": os.Getenv("GCP_ZONE")})

	return string(credJSON), err
}

// gcpCreate creates a billable VM (with its boot disk and an external address): scratch
// projects only, and only with -yes.
func gcpCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 12 || flag.Arg(11) != "-yes" {
		return "", errors.New("usage: gcp create TEMPLATE ROLE HOSTNAME DISK PROJECT ZONE TYPE IMAGE SUBNET -yes (creates a billable VM)")
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"project": flag.Arg(6), "zone": flag.Arg(7), "machineType": flag.Arg(8), "image": flag.Arg(9),
		"subnetwork": flag.Arg(10), "associatePublicIp": true,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartGCPCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// gcpImage makes a billable image with a throwaway helper VM (with a public address): scratch
// projects only, and only with -yes.
func gcpImage(e env, credJSON string) (string, error) {
	if flag.NArg() != 7 || flag.Arg(6) != "-yes" {
		return "", errors.New("usage: gcp image TEMPLATE ARCH ZONE SUBNET -yes (runs a billable VM and keeps an image)")
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "arch": flag.Arg(3), "zone": flag.Arg(4), "subnetwork": flag.Arg(5), "associatePublicIp": true,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartGCPImage(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// azureCreate creates a billable VM (with a NIC and a public address): scratch subscriptions
// only, and only with -yes. NSG_ID "-" for none.
func azureCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 13 || flag.Arg(12) != "-yes" {
		return "", errors.New("usage: azure create TEMPLATE ROLE HOSTNAME DISK RG LOCATION SIZE IMAGE_ID SUBNET_ID NSG_ID|- -yes (creates a billable VM)")
	}

	nsg := flag.Arg(11)
	if nsg == "-" {
		nsg = ""
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"resourceGroup": flag.Arg(6), "location": flag.Arg(7), "size": flag.Arg(8), "imageId": flag.Arg(9),
		"subnetId": flag.Arg(10), "securityGroupId": nsg, "associatePublicIp": true,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartAzureCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// azureImage makes a billable managed image with a throwaway helper VM: scratch subscriptions
// only, and only with -yes. NSG_ID "-" for none.
func azureImage(e env, credJSON string) (string, error) {
	if flag.NArg() != 9 || flag.Arg(8) != "-yes" {
		return "", errors.New("usage: azure image TEMPLATE ARCH RG LOCATION SUBNET_ID NSG_ID|- -yes (runs a billable VM and keeps a managed image)")
	}

	nsg := flag.Arg(7)
	if nsg == "-" {
		nsg = ""
	}

	spec, err := json.Marshal(map[string]string{
		"templateNode": flag.Arg(2), "arch": flag.Arg(3), "resourceGroup": flag.Arg(4), "location": flag.Arg(5),
		"subnetId": flag.Arg(6), "securityGroupId": nsg,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartAzureImage(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// awsCreate runs a billable instance: scratch accounts only, and only with -yes.
func awsCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 11 || flag.Arg(10) != "-yes" {
		return "", errors.New("usage: aws create TEMPLATE ROLE HOSTNAME DISK TYPE AMI SUBNET SG[,SG] -yes (runs a billable instance)")
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"instanceType": flag.Arg(6), "imageId": flag.Arg(7), "subnetId": flag.Arg(8),
		"securityGroupIds": strings.Split(flag.Arg(9), ","), "region": os.Getenv("AWS_REGION"),
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartAWSCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// awsImage makes a billable AMI with a throwaway helper instance: scratch accounts only, and only
// with -yes. SG "-" for the VPC's default group.
func awsImage(e env, credJSON string) (string, error) {
	if flag.NArg() != 7 || flag.Arg(6) != "-yes" {
		return "", errors.New("usage: aws image TEMPLATE ARCH SUBNET SG|- -yes (runs a billable instance and keeps a snapshot and an AMI)")
	}

	group := flag.Arg(5)
	if group == "-" {
		group = ""
	}

	spec, err := json.Marshal(map[string]string{
		"templateNode": flag.Arg(2), "arch": flag.Arg(3), "subnetId": flag.Arg(4), "securityGroupId": group, "region": os.Getenv("AWS_REGION"),
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartAWSImage(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// awsSignIn runs the IAM Identity Center sign-in of the EC2 create: the code and link, the wait
// for the browser, the accounts and roles; with ACCOUNT ROLE, whose the role's credentials are.
// The credential JSON (tokens and keys) is never printed.
func awsSignIn() (string, error) {
	if flag.NArg() != 4 && flag.NArg() != 6 {
		return "", errors.New("usage: aws signin START_URL SSO_REGION [ACCOUNT ROLE]")
	}

	cred, err := json.Marshal(map[string]string{"ssoStartUrl": flag.Arg(2), "ssoRegion": flag.Arg(3), "region": os.Getenv("AWS_REGION")})
	if err != nil {
		return "", err
	}

	var start struct {
		VerificationURIComplete string `json:"verificationUriComplete"`
		UserCode                string `json:"userCode"`
		Credential              string `json:"credential"`
	}

	if err := probeJSON(ichorgo.AWSSignInStart(string(cred)))(&start); err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "open %s and confirm the code %s\n", start.VerificationURIComplete, start.UserCode)

	var finish struct {
		Credential string          `json:"credential"`
		Roles      json.RawMessage `json:"roles"`
	}

	if err := probeJSON(ichorgo.AWSSignInFinish(start.Credential))(&finish); err != nil {
		return "", err
	}

	if flag.NArg() == 4 {
		return string(finish.Roles), nil
	}

	var chosen struct {
		Account string `json:"account"`
		Arn     string `json:"arn"`
	}

	if err := probeJSON(ichorgo.AWSSignInChoose(finish.Credential, flag.Arg(4), flag.Arg(5)))(&chosen); err != nil {
		return "", err
	}

	return "signed in as " + chosen.Arn + " (account " + chosen.Account + ")", nil
}

// probeJSON decodes a facade result, or passes its error on.
func probeJSON(out string, err error) func(any) error {
	return func(v any) error {
		if err != nil {
			return err
		}

		return json.Unmarshal([]byte(out), v)
	}
}

// ovhCreate creates a billable instance: scratch projects only, and only with -yes.
func ovhCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 10 || flag.Arg(9) != "-yes" {
		return "", errors.New("usage: ovh create TEMPLATE ROLE HOSTNAME DISK REGION FLAVOR_ID IMAGE_ID -yes (creates a billable instance)")
	}

	spec, err := json.Marshal(map[string]string{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"region": flag.Arg(6), "flavorId": flag.Arg(7), "imageId": flag.Arg(8),
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartOVHCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// scalewayCreate creates a billable Instance: scratch projects only, and only with -yes.
func scalewayCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 10 || flag.Arg(9) != "-yes" {
		return "", errors.New("usage: scaleway create TEMPLATE ROLE HOSTNAME DISK ZONE TYPE IMAGE_ID -yes (creates a billable Instance, a flexible IP and a volume)")
	}

	spec, err := json.Marshal(map[string]string{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"zone": flag.Arg(6), "type": flag.Arg(7), "imageId": flag.Arg(8),
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartScalewayCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// hetznerSnapshot makes a billable snapshot with a throwaway server: scratch projects only, and
// only with -yes.
func hetznerSnapshot(e env, tokenJSON string) (string, error) {
	if flag.NArg() != 5 || flag.Arg(4) != "-yes" {
		return "", errors.New("usage: hetzner snapshot ARCH LOCATION -yes (creates a billable server and a snapshot)")
	}

	spec, err := json.Marshal(map[string]string{"arch": flag.Arg(2), "location": flag.Arg(3)})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartHetznerSnapshot(e.cfg, e.context, tokenJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// hetznerCreate creates a billable server: scratch projects only, and only with -yes.
func hetznerCreate(e env, tokenJSON string) (string, error) {
	if flag.NArg() != 10 || flag.Arg(9) != "-yes" {
		return "", errors.New("usage: hetzner create TEMPLATE ROLE HOSTNAME DISK TYPE LOCATION SNAPSHOT_ID -yes (creates a billable server)")
	}

	snapshot, err := strconv.ParseInt(flag.Arg(8), 10, 64)
	if err != nil {
		return "", errors.New("SNAPSHOT_ID is a number")
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5), "labelsJson": e.labels,
		"serverType": flag.Arg(6), "location": flag.Arg(7), "snapshotId": snapshot,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartHetznerCreate(e.cfg, e.context, tokenJSON, string(spec), j)

	return "done: " + <-j.done, nil
}
