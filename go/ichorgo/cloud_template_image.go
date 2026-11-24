package ichorgo

import (
	"context"
	"fmt"
	"strings"
)

// templateImage is the cluster's Talos version and the template member's schematic: what a cloud
// image must carry to be the cluster's image.
type templateImage struct {
	version, schematic string
}

// templateImageWords end the warnings of templateImageOf: noVersion when the version cannot be
// read, noSchematic when only the version can.
type templateImageWords struct {
	noVersion, noSchematic string
}

// clusterTemplateImage reads the template member the way cloudImageMedia does, with warnings for
// what cannot be read instead of an error.
func clusterTemplateImage(configYAML, contextName string, words templateImageWords) (templateImage, []string) {
	if strings.TrimSpace(configYAML) == "" {
		return templateImage{}, []string{"no cluster given: " + words.noVersion}
	}

	src, err := withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (bootSource, error) {
		return readBootSource(ctx, s, "")
	})

	return templateImageOf(src, err, words)
}

// templateImageOf is what src says of the cluster's image. A member that cannot be read gives
// neither; an install image that cannot be read gives the version only, so that no image is said
// to match rather than one of the default schematic by mistake.
func templateImageOf(src bootSource, err error, words templateImageWords) (templateImage, []string) {
	media := bootMedia{}
	if err == nil {
		media, err = buildBootMedia(src, "", false)
	}

	if err != nil {
		return templateImage{}, []string{"the cluster's Talos version cannot be read (" + friendlyError(err) + "): " + words.noVersion}
	}

	if src.imageErr != "" {
		return templateImage{version: media.TalosVersion},
			[]string{fmt.Sprintf("the install image of %s cannot be read (%s): %s", src.node, src.imageErr, words.noSchematic)}
	}

	return templateImage{version: media.TalosVersion, schematic: media.Schematic}, nil
}
