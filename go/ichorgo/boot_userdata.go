package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// S8 step 4: the user data of a new node on a cloud host. Hetzner, OpenStack (OVHcloud Public
// Cloud) and Scaleway create a server from an image plus a user_data text, and Talos's cloud
// platforms read that text as the machine config. For an Omni context it is Omni's join config
// (the machine registers over SideroLink and appears unallocated in Omni); for a plain Talos
// context it is the full config of a new member, made from a template member's
// (JoinNodeConfigSealed). Nothing is sent to a provider here: the person pastes the text.

const (
	cloudUserDataOmni  = "omni"
	cloudUserDataTalos = "talos"

	// cloudUserDataLimit is Hetzner's user_data limit (POST /servers), the strictest of the
	// clouds the app names; OVHcloud documents none. Above it: a warning, not a refusal.
	cloudUserDataLimit = 32 << 10

	cloudPlatformHetzner   = "hcloud"
	cloudPlatformOpenStack = "openstack"
	cloudPlatformScaleway  = "scaleway"
)

var (
	errCloudOmniPrivacy = errors.New("turn privacy mode off to see Omni's join config: it carries the join token")

	// omniJoinTokenPattern finds the join token in Omni's join config (siderolink apiUrl).
	omniJoinTokenPattern = regexp.MustCompile(`(jointoken=)[^&\s'"]+`)
)

// cloudUserData is what NodeCloudUserData returns. UserData is a credential (Omni's join
// token, or the cluster's CAs and tokens): never logged, never in a QR code.
type cloudUserData struct {
	Kind            string       `json:"kind"` // omni or talos
	UserData        string       `json:"userData"`
	Size            int          `json:"size"`
	Limit           int          `json:"limit"`
	RedactedPreview string       `json:"redactedPreview"`
	Role            string       `json:"role"`
	Hostname        string       `json:"hostname"`
	TemplateNode    string       `json:"templateNode"`
	TalosVersion    string       `json:"talosVersion"`
	Schematic       string       `json:"schematic"`
	Images          []cloudImage `json:"images"`
	Warnings        []string     `json:"warnings"`
}

// cloudImage is the Image Factory disk image to create the server from on one platform.
type cloudImage struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
}

// NodeCloudUserData returns the user data of a new node on a cloud host, to paste when the
// server is created, and the Image Factory cloud images of the cluster's Talos version and
// schematic (hcloud, openstack, scaleway, the template's architecture). On an Omni context it
// is Omni's join config (role, hostname and installDisk are not used: Omni picks the role when
// the machine is allocated). On a plain Talos context it is the full config of a new member
// made from templateNode's (os:admin, as JoinNodeConfigSealed: role worker or controlplane,
// hostname and installDisk required, extraEditsJSON as there), recorded in the activity log.
// Both carry a credential: refused in privacy mode. JSON: {"kind","userData","size","limit",
// "redactedPreview","role","hostname","templateNode","talosVersion","schematic",
// "images":[{"platform","arch","url"}],"warnings"}.
func NodeCloudUserData(configYAML, contextName, templateNode, role, hostname, installDisk, extraEditsJSON string) (out string, err error) {
	// Only the errors are masked: the text is the point, and privacy mode refuses it first.
	defer maskErr(&err)

	contextName, templateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(templateNode))

	if !isTalosDemoContext(configYAML, contextName) {
		_, cfgCtx, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		if isOmni(cfgCtx) {
			if privacy.isEnabled() {
				return "", errCloudOmniPrivacy
			}

			data, err := withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (cloudUserData, error) {
				return omniCloudUserData(ctx, s, templateNode, role, hostname, installDisk, nil)
			})
			if err != nil {
				return "", err
			}

			return toJSON(data)
		}
	}

	if privacy.isEnabled() {
		return "", errJoinPrivacy
	}

	defer recordAction(&err, configYAML, contextName,
		auditAction{Action: "cloud-user-data-generated", Node: templateNode, Params: "hostname=" + strings.TrimSpace(hostname) + " role=" + role})

	data, err := talosCloudUserData(configYAML, contextName, templateNode, role, hostname, installDisk, extraEditsJSON)
	if err != nil {
		return "", err
	}

	return toJSON(data)
}

// NodeCloudUserDataOmni is NodeCloudUserData on an Omni context with labelsJSON ({"key":"value"},
// "" for none) signed into the join token: the machine appears in Omni with them, and a machine
// class can pick it by them. Omni's own prefix is refused, as is another kind of context, and
// privacy mode: the text carries the join token.
func NodeCloudUserDataOmni(configYAML, contextName, templateNode, labelsJSON string) (out string, err error) {
	// Only the errors are masked: the text is the point, and privacy mode refuses it first.
	defer maskErr(&err)

	contextName, templateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(templateNode))

	labels, err := parseOmniLabels(labelsJSON)
	if err != nil {
		return "", err
	}

	if privacy.isEnabled() {
		return "", errCloudOmniPrivacy
	}

	_, cfgCtx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	if !isOmni(cfgCtx) {
		return "", errNotOmni
	}

	data, err := withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (cloudUserData, error) {
		return omniCloudUserData(ctx, s, templateNode, "", "", "", labels)
	})
	if err != nil {
		return "", err
	}

	return toJSON(data)
}

// omniCloudUserData is Omni's join config, with the cloud images of the template member's
// version and schematic when a member answers (the user data stays useful without them).
func omniCloudUserData(ctx context.Context, s *session, templateNode, role, hostname, installDisk string, labels map[string]string) (cloudUserData, error) {
	data := cloudUserData{Kind: cloudUserDataOmni, Warnings: []string{}}

	if strings.TrimSpace(role+hostname+installDisk) != "" {
		data.Warnings = append(data.Warnings, "Omni picks the role, the host name and the install disk when the machine is allocated: the ones given are not used")
	}

	if src, err := readBootSource(ctx, s, templateNode); err != nil {
		data.Warnings = append(data.Warnings, "no cloud image to list: "+err.Error())
	} else if media, err := buildBootMedia(src, "", false); err != nil {
		data.Warnings = append(data.Warnings, "no cloud image to list: "+err.Error())
	} else {
		data.withImages(media)
	}

	join, err := omniJoinConfig(ctx, s.context, s.signing.current(), labels)
	if err != nil {
		return cloudUserData{}, err
	}

	if strings.TrimSpace(join.config) == "" {
		return cloudUserData{}, errors.New("omni returned no join config: update Omni, or boot the machine with the join kernel arguments instead")
	}

	data.UserData = join.config
	data.RedactedPreview = omniJoinTokenPattern.ReplaceAllString(join.config, "${1}"+redacted)
	data.finish()

	return data, nil
}

// talosCloudUserData is a new member's full config made from templateNode's, with the cloud
// images of that member's version and schematic.
func talosCloudUserData(configYAML, contextName, templateNode, role, hostname, installDisk, extraEditsJSON string) (cloudUserData, error) {
	req, err := parseJoinRequest(templateNode, role, hostname, installDisk, "", extraEditsJSON)
	if err != nil {
		return cloudUserData{}, err
	}

	media, err := cloudBootMedia(configYAML, contextName, req.template)
	if err != nil {
		return cloudUserData{}, err
	}

	full, warnings, err := joinConfigFor(configYAML, contextName, req)
	if err != nil {
		return cloudUserData{}, err
	}

	preview, err := redactConfig(full)
	if err != nil {
		return cloudUserData{}, err
	}

	data := cloudUserData{
		Kind: cloudUserDataTalos, UserData: full, RedactedPreview: preview,
		Role: req.role, Hostname: req.hostname, TemplateNode: req.template,
		Warnings: append(warnings, media.Warnings...),
	}
	data.withImages(media)
	data.finish()

	return data, nil
}

// cloudBootMedia is the template's version, schematic and architecture (NodeBootMedia's).
func cloudBootMedia(configYAML, contextName, templateNode string) (bootMedia, error) {
	if isTalosDemoContext(configYAML, contextName) {
		return demoBootMedia(templateNode, "", false), nil
	}

	return withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (bootMedia, error) {
		src, err := readBootSource(ctx, s, templateNode)
		if err != nil {
			return bootMedia{}, err
		}

		return buildBootMedia(src, "", false)
	})
}

// withImages lists the cloud images of media's target.
func (d *cloudUserData) withImages(media bootMedia) {
	target := media.target()
	target.secureBoot = false

	d.TalosVersion, d.Schematic = media.TalosVersion, media.Schematic
	if d.TemplateNode == "" {
		d.TemplateNode = media.TemplateNode
	}

	d.Images = cloudImages(target)
	d.Warnings = append(d.Warnings, "Scaleway: follow the Talos guide's disk layout (a second disk for EPHEMERAL); some instance types need apply-config instead of user data")
}

func cloudImages(t bootTarget) []cloudImage {
	platforms := []string{cloudPlatformHetzner, cloudPlatformOpenStack, cloudPlatformScaleway}
	images := make([]cloudImage, 0, len(platforms))

	for _, p := range platforms {
		images = append(images, cloudImage{Platform: p, Arch: t.arch, URL: cloudImageURL(t, p)})
	}

	return images
}

// finish sets the size and the limit, and warns above it.
func (d *cloudUserData) finish() {
	d.Size, d.Limit = len(d.UserData), cloudUserDataLimit
	if d.Images == nil {
		d.Images = []cloudImage{}
	}

	if d.Size > d.Limit {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d bytes is larger than the 32 KiB most clouds accept as user data: remove docs and examples or extra edits", d.Size))
	}
}
