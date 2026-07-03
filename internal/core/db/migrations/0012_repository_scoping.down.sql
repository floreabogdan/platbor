-- Irreversible pre-release reset: 0012 drops and recreates the artifact tables
-- under repository scope. There is no meaningful down migration (the old
-- project-scoped rows are gone), so this is intentionally a no-op.
SELECT 1;
