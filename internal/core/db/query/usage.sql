-- name: ProjectFlatStorageUsage :one
-- A project's logical storage across every flat-sized format (all formats except
-- OCI, whose per-image size is computed from manifest payloads and summed
-- separately). Sizes are summed per artifact row, so a blob shared by two
-- artifacts counts once per artifact -- matching the per-repository sizes shown
-- in the browser. sqlc.arg(project_id) keeps this a single Go parameter across
-- every branch.
SELECT CAST(COALESCE(SUM(sz), 0) AS INTEGER) AS bytes FROM (
    SELECT f.size AS sz FROM generic_files f
      JOIN repositories r ON r.id = f.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT f.size FROM go_files f
      JOIN repositories r ON r.id = f.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT f.size FROM maven_files f
      JOIN repositories r ON r.id = f.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT f.size FROM pypi_files f
      JOIN pypi_packages pkg ON pkg.id = f.package_id
      JOIN repositories r ON r.id = pkg.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT v.tarball_size FROM npm_versions v
      JOIN npm_packages pkg ON pkg.id = v.package_id
      JOIN repositories r ON r.id = pkg.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT v.nupkg_size FROM nuget_versions v
      JOIN nuget_packages pkg ON pkg.id = v.package_id
      JOIN repositories r ON r.id = pkg.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT v.size FROM cargo_versions v
      JOIN cargo_crates c ON c.id = v.crate_id
      JOIN repositories r ON r.id = c.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT v.size FROM gem_versions v
      JOIN gem_gems g ON g.id = v.gem_id
      JOIN repositories r ON r.id = g.repository_id WHERE r.project_id = sqlc.arg(project_id)
    UNION ALL
    SELECT v.size FROM terraform_versions v
      JOIN terraform_modules m ON m.id = v.module_id
      JOIN repositories r ON r.id = m.repository_id WHERE r.project_id = sqlc.arg(project_id)
);
