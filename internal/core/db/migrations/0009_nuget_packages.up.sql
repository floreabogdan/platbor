-- NuGet packages and their versions, scoped to a project. The project IS the
-- NuGet feed: clients point at /nuget/<project>/v3/index.json and packages live
-- directly under it by id. NuGet ids and versions are case-insensitive, so a
-- lowercased form is stored for lookup alongside the original-case form for
-- display and URLs.
--
-- Each version's .nupkg (a zip) lives in the shared content-addressable blob
-- store; the .nuspec (the package manifest XML, extracted from the nupkg at
-- push) is kept here so the registration and metadata resources can be built
-- without re-opening the archive.
CREATE TABLE nuget_packages (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    id_lower    TEXT NOT NULL,          -- lowercased package id, for lookup
    id_original TEXT NOT NULL,          -- original-case id, for display
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (project_id, id_lower)
);

CREATE TABLE nuget_versions (
    id            TEXT PRIMARY KEY,
    package_id    TEXT NOT NULL REFERENCES nuget_packages (id) ON DELETE CASCADE,
    version       TEXT NOT NULL,        -- original version string, e.g. 1.2.3-beta
    version_lower TEXT NOT NULL,        -- lowercased, for lookup
    nupkg_digest  TEXT NOT NULL,        -- sha256:<hex> into the blob store
    nupkg_size    INTEGER NOT NULL,
    nuspec        BLOB NOT NULL,        -- the .nuspec XML extracted from the nupkg
    created_at    TEXT NOT NULL,
    UNIQUE (package_id, version_lower)
);

CREATE INDEX idx_nuget_versions_package ON nuget_versions (package_id);
