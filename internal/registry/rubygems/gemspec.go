package rubygems

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// gemSpec is the parsed subset of a .gem's metadata (the gzipped YAML gemspec)
// that the compact index needs.
type gemSpec struct {
	Name     string
	Version  string
	Platform string
	Number   string // version, or version-platform for non-ruby platforms
	FullName string // name-number; the .gem filename base
	InfoDeps string // "dep:req,dep2:req2" (runtime deps) for the info line
	InfoReqs string // "checksum:...,ruby:...,rubygems:..." for the info line
}

// yamlSpec mirrors the fields we read from the gemspec YAML. Ruby-object tags
// (!ruby/object:...) are ignored by yaml.v3 when decoding into a struct.
type yamlSpec struct {
	Name     string      `yaml:"name"`
	Version  yamlVersion `yaml:"version"`
	Platform string      `yaml:"platform"`
	Deps     []yamlDep   `yaml:"dependencies"`
	RubyReq  yamlReq     `yaml:"required_ruby_version"`
	GemsReq  yamlReq     `yaml:"required_rubygems_version"`
}

type yamlVersion struct {
	Version string `yaml:"version"`
}

type yamlDep struct {
	Name    string  `yaml:"name"`
	Type    string  `yaml:"type"`
	VersReq yamlReq `yaml:"version_requirements"`
	Req     yamlReq `yaml:"requirement"`
}

// yamlReq is a Gem::Requirement: a list of [op, Gem::Version] pairs.
type yamlReq struct {
	Requirements [][]yaml.Node `yaml:"requirements"`
}

// parseGemSpec reads a .gem (an uncompressed tar containing metadata.gz) and
// extracts the compact-index fields. cksum is the sha256 of the whole .gem.
func parseGemSpec(gemBytes []byte, cksum string) (gemSpec, error) {
	metaGz, err := readTarEntry(gemBytes, "metadata.gz")
	if err != nil {
		return gemSpec{}, err
	}
	gz, err := gzip.NewReader(strings.NewReader(string(metaGz)))
	if err != nil {
		return gemSpec{}, fmt.Errorf("opening metadata.gz: %w", err)
	}
	defer func() { _ = gz.Close() }()
	metaYAML, err := io.ReadAll(io.LimitReader(gz, 4<<20))
	if err != nil {
		return gemSpec{}, fmt.Errorf("reading metadata: %w", err)
	}

	var spec yamlSpec
	if err := yaml.Unmarshal(metaYAML, &spec); err != nil {
		return gemSpec{}, fmt.Errorf("parsing gemspec: %w", err)
	}
	if spec.Name == "" || spec.Version.Version == "" {
		return gemSpec{}, fmt.Errorf("gemspec missing name or version")
	}

	platform := spec.Platform
	if platform == "" {
		platform = "ruby"
	}
	number := spec.Version.Version
	if platform != "ruby" {
		number = number + "-" + platform
	}

	out := gemSpec{
		Name:     spec.Name,
		Version:  spec.Version.Version,
		Platform: platform,
		Number:   number,
		FullName: spec.Name + "-" + number,
		InfoDeps: buildDeps(spec.Deps),
		InfoReqs: buildReqs(cksum, spec.RubyReq, spec.GemsReq),
	}
	return out, nil
}

// buildDeps formats the runtime dependencies for a compact-index info line:
// "name:req,name2:req2" where a multi-clause req joins with "&".
func buildDeps(deps []yamlDep) string {
	var parts []string
	for _, d := range deps {
		if d.Type != "" && d.Type != ":runtime" && d.Type != "runtime" {
			continue // development deps are not in the compact index
		}
		req := reqString(pickReq(d.VersReq, d.Req))
		parts = append(parts, d.Name+":"+strings.ReplaceAll(req, ", ", "&"))
	}
	return strings.Join(parts, ",")
}

// buildReqs formats the trailing requirements of an info line: the checksum plus
// any non-trivial ruby / rubygems version requirement.
func buildReqs(cksum string, ruby, gems yamlReq) string {
	parts := []string{"checksum:" + cksum}
	if r := reqString(ruby); r != "" && r != ">= 0" {
		parts = append(parts, "ruby:"+strings.ReplaceAll(r, ", ", "&"))
	}
	if r := reqString(gems); r != "" && r != ">= 0" {
		parts = append(parts, "rubygems:"+strings.ReplaceAll(r, ", ", "&"))
	}
	return strings.Join(parts, ",")
}

func pickReq(a, b yamlReq) yamlReq {
	if len(a.Requirements) > 0 {
		return a
	}
	return b
}

// reqString renders a Gem::Requirement's [op, version] pairs as "op ver, op ver".
func reqString(req yamlReq) string {
	var clauses []string
	for _, pair := range req.Requirements {
		if len(pair) < 2 {
			continue
		}
		var op string
		if err := pair[0].Decode(&op); err != nil {
			continue
		}
		var ver struct {
			Version string `yaml:"version"`
		}
		if err := pair[1].Decode(&ver); err != nil || ver.Version == "" {
			// The version may be a bare scalar in some encodings.
			var scalar string
			if e := pair[1].Decode(&scalar); e == nil && scalar != "" {
				ver.Version = scalar
			} else {
				continue
			}
		}
		clauses = append(clauses, op+" "+ver.Version)
	}
	return strings.Join(clauses, ", ")
}

// readTarEntry returns the bytes of a named entry in an uncompressed tar.
func readTarEntry(tarBytes []byte, name string) ([]byte, error) {
	tr := tar.NewReader(strings.NewReader(string(tarBytes)))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in gem", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading gem tar: %w", err)
		}
		if hdr.Name == name {
			return io.ReadAll(io.LimitReader(tr, 8<<20))
		}
	}
}
