package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"go.yaml.in/yaml/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// Step 0 of joining a node from the phone: what boots the new machine into Talos, so it shows
// up in maintenance mode for step 1 (MaintenanceNodeInspect). See boot_media_urls.go.

const (
	schematicFromMember  = "member"
	schematicFromDefault = "default"

	omniJoinConfigMethod = "/management.ManagementService/GetMachineJoinConfig"
	// omniDefaultJoinTokenType is Omni's resource naming the join token machines use unless
	// told otherwise (what `omnictl jointoken kernel-args` reads first).
	omniDefaultJoinTokenType = "DefaultJoinTokens.omni.sidero.dev"
)

// demoBootSchematic is an invented schematic for the demo cluster.
const demoBootSchematic = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"

// bootMedia is what NodeBootMedia returns.
type bootMedia struct {
	TalosVersion    string `json:"talosVersion"`
	Schematic       string `json:"schematic"`
	SchematicSource string `json:"schematicSource"` // member or default
	Arch            string `json:"arch"`
	SecureBoot      bool   `json:"secureBoot"`
	TemplateNode    string `json:"templateNode"`
	bootLinks
	BooterDocker string         `json:"booterDocker"`
	BooterPodman string         `json:"booterPodman"`
	Omni         *bootMediaOmni `json:"omni"`
	Warnings     []string       `json:"warnings"`
}

// bootMediaOmni is how a new machine joins an Omni instance. KernelArgs carries the join
// token: a credential, never logged, and left out in privacy mode.
type bootMediaOmni struct {
	KernelArgs   string `json:"kernelArgs"`
	BooterDocker string `json:"booterDocker"`
	BooterPodman string `json:"booterPodman"`
}

// bootSource is what the template member says about itself.
type bootSource struct {
	node, version, arch string
	image, imageErr     string // machine.install.image, or why it could not be read
}

// NodeBootMedia returns what boots a new machine of the cluster into Talos: the template
// member's Talos version and Image Factory schematic, the ISO, iPXE, kernel, initramfs and
// command-line URLs, and the siderolabs/booter command (docker and podman) that serves a PXE
// boot from a laptop of the machine's network. On an Omni context it adds Omni's join kernel
// arguments and the booter command with them (left out in privacy mode: they carry the join
// token). templateNode "" takes the first member that answers; arch is amd64, arm64 or "" for
// the template's. JSON: {"talosVersion","schematic","schematicSource","arch","secureBoot",
// "templateNode","iso","ipxe","kernel","initramfs","cmdline","booterDocker","booterPodman",
// "omni":{"kernelArgs","booterDocker","booterPodman"}|null,"warnings"}. The schematic needs
// os:admin (the member's machine config); with less, the default schematic and a warning.
func NodeBootMedia(configYAML, contextName, templateNode, arch string, secureBoot bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, templateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(templateNode))

	arch, err = normalizeBootArch(arch)
	if err != nil {
		return "", err
	}

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoBootMedia(templateNode, arch, secureBoot))
	}

	media, err := withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (bootMedia, error) {
		src, err := readBootSource(ctx, s, templateNode)
		if err != nil {
			return bootMedia{}, err
		}

		media, err := buildBootMedia(src, arch, secureBoot)
		if err != nil || !isOmni(s.context) {
			return media, err
		}

		addOmniJoin(ctx, s, &media)

		return media, nil
	})
	if err != nil {
		return "", err
	}

	return toJSON(media)
}

// readBootSource asks templateNode (or the context's members in turn) for its version, its
// architecture and its install image.
func readBootSource(ctx context.Context, s *session, templateNode string) (bootSource, error) {
	candidates := targetNodes(s.context)

	if templateNode != "" {
		if err := validatePowerTarget(s.context, templateNode); err != nil {
			return bootSource{}, err
		}

		candidates = []string{templateNode}
	}

	if len(candidates) == 0 {
		return bootSource{}, errors.New("this context names no node to read the Talos version from")
	}

	var lastErr error

	for _, node := range candidates {
		nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
		resp, err := s.client.Version(client.WithNode(nodeCtx, node))

		cancel()

		if err != nil {
			lastErr = err

			continue
		}

		v := first(resp.GetMessages()).GetVersion()
		src := bootSource{node: node, version: v.GetTag(), arch: v.GetArch()}
		src.image, src.imageErr = installerImage(ctx, s.client, node)

		return src, nil
	}

	return bootSource{}, fmt.Errorf("no member answers: %s", friendlyError(lastErr))
}

// buildBootMedia picks the version, schematic and architecture out of src (the request's
// arch first) and builds the links and the commands.
func buildBootMedia(src bootSource, arch string, secureBoot bool) (bootMedia, error) {
	media := bootMedia{TemplateNode: src.node, SecureBoot: secureBoot, Warnings: []string{}}

	media.Schematic, media.SchematicSource = imageSchematic(src.image), schematicFromMember
	if media.Schematic == "" {
		media.Schematic, media.SchematicSource = defaultSchematic, schematicFromDefault
		media.Warnings = append(media.Warnings, defaultSchematicWarning(src))
	}

	// The running version, not the install image's tag: an upgrade by image does not always
	// rewrite machine.install.image, and the new machine should run what the cluster runs.
	_, tag := splitImageRef(src.image)
	tagVersion, tagOK := normalizeTalosVersion(tag)
	running, runningOK := normalizeTalosVersion(src.version)

	switch {
	case runningOK:
		media.TalosVersion = running
		if tagOK && tagVersion != running {
			media.Warnings = append(media.Warnings, fmt.Sprintf("%s runs %s but its install image is %s: the media boot %s", src.node, running, tagVersion, running))
		}
	case tagOK:
		media.TalosVersion = tagVersion
	default:
		return media, fmt.Errorf("%s did not say which Talos version it runs", src.node)
	}

	media.Arch = cmpOr(arch, cmpOr(src.arch, bootArchAMD64))
	if a, err := normalizeBootArch(media.Arch); err == nil && a != "" {
		media.Arch = a
	}

	target := media.target()
	media.bootLinks = bootMediaLinks(target)
	media.BooterDocker = booterCommand("docker", target, "")
	media.BooterPodman = booterCommand("podman", target, "")

	return media, nil
}

func (m bootMedia) target() bootTarget {
	return bootTarget{version: m.TalosVersion, schematic: m.Schematic, arch: m.Arch, secureBoot: m.SecureBoot}
}

func defaultSchematicWarning(src bootSource) string {
	const without = "the media use the Image Factory's default schematic, without the template's extensions"

	switch {
	case src.imageErr != "":
		return fmt.Sprintf("the install image of %s cannot be read (%s): %s", src.node, src.imageErr, without)
	case src.image == "":
		return fmt.Sprintf("%s names no install image: %s", src.node, without)
	}

	return fmt.Sprintf("%s installs %s, not an Image Factory image: %s", src.node, src.image, without)
}

// addOmniJoin adds Omni's join kernel arguments to media, or a warning saying why not: the
// plain Talos media stay useful without them.
func addOmniJoin(ctx context.Context, s *session, media *bootMedia) {
	if privacy.isEnabled() {
		media.Warnings = append(media.Warnings, "turn privacy mode off to see Omni's join arguments: they carry the join token")

		return
	}

	args, err := omniJoinKernelArgs(ctx, s.context, s.signing.current())
	if err != nil {
		media.Warnings = append(media.Warnings, "Omni's join arguments cannot be read: "+err.Error())

		return
	}

	target := media.target()
	media.Omni = &bootMediaOmni{
		KernelArgs:   args,
		BooterDocker: booterCommand("docker", target, args),
		BooterPodman: booterCommand("podman", target, args),
	}
}

// omniJoinKernelArgs is `omnictl jointoken kernel-args`: the default join token, then
// GetMachineJoinConfig with it, its kernel_args joined by spaces. See Omni's
// client/api/omni/management/management.proto: GetMachineJoinConfigRequest {bool
// use_grpc_tunnel = 1; string join_token = 2; map machine_labels = 3}, Response {repeated
// string kernel_args = 1; string config = 2} (config, a credential too, is not read).
func omniJoinKernelArgs(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner) (string, error) {
	cc, err := omniDial(cfgCtx, &omniSigning{method: omniMethodName(cfgCtx), signer: signer})
	if err != nil {
		return "", fmt.Errorf("connect to Omni: %w", err)
	}

	defer cc.Close() //nolint:errcheck

	token, err := omniDefaultJoinToken(ctx, cc)
	if err != nil {
		return "", omniJoinError(err)
	}

	var req []byte
	req = protowire.AppendTag(req, 2, protowire.BytesType)
	req = protowire.AppendString(req, token)

	resp, err := omniInvoke(ctx, cc, omniJoinConfigMethod, req)
	if err != nil {
		return "", omniJoinError(err)
	}

	values, err := protoFields(resp, 1)
	if err != nil {
		return "", fmt.Errorf("read Omni's join config: %w", err)
	}

	args := make([]string, 0, len(values))
	for _, v := range values {
		args = append(args, string(v))
	}

	if len(args) == 0 {
		return "", errors.New("omni returned no join argument")
	}

	return strings.Join(args, " "), nil
}

// omniDefaultJoinToken reads the ID of the instance's default join token from its state.
func omniDefaultJoinToken(ctx context.Context, cc *grpc.ClientConn) (string, error) {
	resp, err := v1alpha1.NewStateClient(cc).Get(ctx, &v1alpha1.GetRequest{
		Namespace: "default",
		Type:      omniDefaultJoinTokenType,
		Id:        "default",
		Options:   &v1alpha1.GetOptions{},
	})
	if err != nil {
		return "", err
	}

	var spec map[string]any
	if err := yaml.Unmarshal([]byte(resp.GetResource().GetSpec().GetYamlSpec()), &spec); err != nil {
		return "", fmt.Errorf("read Omni's default join token: %w", err)
	}

	for key, value := range spec {
		if s, ok := value.(string); ok && strings.ReplaceAll(strings.ToLower(key), "_", "") == "tokenid" && s != "" {
			return s, nil
		}
	}

	return "", errors.New("omni has no default join token")
}

func omniJoinError(err error) error {
	if status.Code(err) == codes.PermissionDenied {
		return errors.New("your Omni role cannot read the join token: ask for the Operator role")
	}

	return omniAPIError(err)
}

// demoBootMedia is the demo cluster's: an invented schematic, the demo's version.
func demoBootMedia(templateNode, arch string, secureBoot bool) bootMedia {
	src := bootSource{
		node:    cmpOr(templateNode, demoNodes()[0].Node),
		version: demoTalosVersion,
		arch:    bootArchAMD64,
		image:   factoryHost + "/metal-installer/" + demoBootSchematic + ":" + demoTalosVersion,
	}

	media, _ := buildBootMedia(src, arch, secureBoot)

	return media
}
