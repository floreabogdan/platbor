package nuget

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// nuspec is the subset of a .nuspec manifest Platbor reads: identity plus the
// dependency groups, which the registration resource surfaces so the client can
// resolve a restore.
type nuspec struct {
	Metadata struct {
		ID           string `xml:"id"`
		Version      string `xml:"version"`
		Description  string `xml:"description"`
		Authors      string `xml:"authors"`
		Dependencies struct {
			// A package may declare dependencies either grouped by target
			// framework or as a flat list; both are captured.
			Groups []struct {
				TargetFramework string             `xml:"targetFramework,attr"`
				Dependencies    []nuspecDependency `xml:"dependency"`
			} `xml:"group"`
			Flat []nuspecDependency `xml:"dependency"`
		} `xml:"dependencies"`
	} `xml:"metadata"`
}

type nuspecDependency struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
	Exclude string `xml:"exclude,attr"`
}

// parseNupkg reads a .nupkg (a zip) and returns the package id, version, and the
// raw .nuspec XML found at the archive root. NuGet places exactly one
// "<id>.nuspec" at the root of the package.
func parseNupkg(data []byte) (id, version string, nuspecXML []byte, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", "", nil, fmt.Errorf("reading nupkg zip: %w", err)
	}
	for _, f := range zr.File {
		// The manifest is a *.nuspec at the archive root (no path separator).
		if !strings.HasSuffix(strings.ToLower(f.Name), ".nuspec") || strings.ContainsAny(f.Name, "/\\") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", "", nil, fmt.Errorf("opening nuspec: %w", err)
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 4<<20))
		_ = rc.Close()
		if err != nil {
			return "", "", nil, fmt.Errorf("reading nuspec: %w", err)
		}
		var spec nuspec
		if err := xml.Unmarshal(raw, &spec); err != nil {
			return "", "", nil, fmt.Errorf("parsing nuspec: %w", err)
		}
		if spec.Metadata.ID == "" || spec.Metadata.Version == "" {
			return "", "", nil, fmt.Errorf("nuspec missing id or version")
		}
		return spec.Metadata.ID, spec.Metadata.Version, raw, nil
	}
	return "", "", nil, fmt.Errorf("no .nuspec found in package")
}

// parseNuspec unmarshals a stored .nuspec for building registration metadata.
func parseNuspec(raw []byte) (nuspec, error) {
	var spec nuspec
	if err := xml.Unmarshal(raw, &spec); err != nil {
		return nuspec{}, err
	}
	return spec, nil
}
