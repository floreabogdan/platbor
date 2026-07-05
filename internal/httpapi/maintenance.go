package httpapi

import (
	"database/sql"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/cargo"
	"github.com/platbor/platbor/internal/registry/generic"
	"github.com/platbor/platbor/internal/registry/goproxy"
	"github.com/platbor/platbor/internal/registry/maven"
	"github.com/platbor/platbor/internal/registry/npm"
	"github.com/platbor/platbor/internal/registry/nuget"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/registry/pypi"
	"github.com/platbor/platbor/internal/registry/rubygems"
	"github.com/platbor/platbor/internal/registry/terraform"
)

// newCollector builds the cross-format garbage collector. It unions every
// format's blob referencers so a sweep never deletes content that is live in any
// format — a missing referencer would delete real data, so this list must grow
// with every new format. The registry API handler and the background scheduler
// share this one builder.
func newCollector(sqlDB *sql.DB, blobs blob.Store) *oci.Collector {
	return oci.NewCollector(blobs, sqlDB,
		npm.NewReferencer(sqlDB),
		generic.NewReferencer(sqlDB),
		nuget.NewReferencer(sqlDB),
		pypi.NewReferencer(sqlDB),
		maven.NewReferencer(sqlDB),
		goproxy.NewReferencer(sqlDB),
		cargo.NewReferencer(sqlDB),
		rubygems.NewReferencer(sqlDB),
		terraform.NewReferencer(sqlDB),
	)
}

// newRetention builds the retention engine with each format's pruner.
func newRetention(sqlDB *sql.DB) *RetentionService {
	return NewRetentionService(sqlDB, map[repository.Format]registry.Pruner{
		repository.FormatOCI:       oci.NewPruner(sqlDB),
		repository.FormatNPM:       npm.NewPruner(sqlDB),
		repository.FormatNuGet:     nuget.NewPruner(sqlDB),
		repository.FormatPyPI:      pypi.NewPruner(sqlDB),
		repository.FormatMaven:     maven.NewPruner(sqlDB),
		repository.FormatGo:        goproxy.NewPruner(sqlDB),
		repository.FormatCargo:     cargo.NewPruner(sqlDB),
		repository.FormatRubyGems:  rubygems.NewPruner(sqlDB),
		repository.FormatTerraform: terraform.NewPruner(sqlDB),
	})
}
