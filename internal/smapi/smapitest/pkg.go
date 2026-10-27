package smapitest

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// parsePackage checks a skill package roughly as SMAPI does: a manifest, and a publishing locale with a name for every interaction model.
func parsePackage(data []byte) (*skillPackage, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("package is not a zip file: %w", err)
	}

	pkg := &skillPackage{names: map[string]string{}, models: map[string]json.RawMessage{}}

	for _, f := range zr.File {
		b, err := readZipFile(f)
		if err != nil {
			return nil, err
		}

		switch {
		case f.Name == "skill-package/skill.json":
			pkg.manifest = b
		case strings.HasPrefix(f.Name, "skill-package/interactionModels/custom/"):
			if !json.Valid(b) {
				return nil, fmt.Errorf("%s is not valid JSON", f.Name)
			}

			pkg.models[strings.TrimSuffix(path.Base(f.Name), ".json")] = b
		}
	}

	if pkg.manifest == nil {
		return nil, errors.New("skill-package/skill.json is missing")
	}

	var m struct {
		Manifest struct {
			PublishingInformation struct {
				Locales map[string]struct {
					Name string `json:"name"`
				} `json:"locales"`
			} `json:"publishingInformation"`
		} `json:"manifest"`
	}

	if err := json.Unmarshal(pkg.manifest, &m); err != nil {
		return nil, fmt.Errorf("skill.json: %w", err)
	}

	for loc, info := range m.Manifest.PublishingInformation.Locales {
		pkg.names[loc] = info.Name
	}

	for loc := range pkg.models {
		if pkg.names[loc] == "" {
			return nil, fmt.Errorf("interaction model locale %s has no publishingInformation.locales entry with a name", loc)
		}
	}

	return pkg, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(io.LimitReader(rc, 8<<20))
}
