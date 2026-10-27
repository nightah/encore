package buildinfo

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

var labelKey = regexp.MustCompile(`([A-Za-z0-9_.-]+)=`)

// dockerfileLabels returns the keys the LABEL instructions after the last FROM set, joining continued lines.
func dockerfileLabels(t *testing.T, path string) map[string]bool {
	t.Helper()

	b, err := os.ReadFile(path)
	require.NoError(t, err)

	var stage []string

	for line := range strings.SplitSeq(strings.ReplaceAll(string(b), "\\\n", " "), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FROM ") {
			stage = nil
		}

		stage = append(stage, line)
	}

	keys := map[string]bool{}

	for _, line := range stage {
		if rest, ok := strings.CutPrefix(line, "LABEL "); ok {
			for _, m := range labelKey.FindAllStringSubmatch(rest, -1) {
				keys[m[1]] = true
			}
		}
	}

	return keys
}

// The base image labels itself with its own version and build date, so a label the release image doesn't set is inherited from the base and describes the wrong image.
func TestReleaseImageSetsTheLocalImageLabels(t *testing.T) {
	local := dockerfileLabels(t, "../../Dockerfile")
	require.Contains(t, local, "version")

	release := dockerfileLabels(t, "../../Dockerfile.goreleaser")

	b, err := os.ReadFile("../../.goreleaser.yaml")
	require.NoError(t, err)

	var cfg struct {
		DockersV2 []struct {
			Labels map[string]string `yaml:"labels"`
		} `yaml:"dockers_v2"`
	}
	require.NoError(t, yaml.Unmarshal(b, &cfg))
	require.Len(t, cfg.DockersV2, 1)

	for key := range cfg.DockersV2[0].Labels {
		release[key] = true
	}

	for key := range local {
		assert.True(t, release[key], "the release image doesn't set the %s label that ./Dockerfile does", key)
	}
}
