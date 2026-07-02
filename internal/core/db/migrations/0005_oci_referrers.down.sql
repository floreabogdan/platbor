DROP INDEX IF EXISTS idx_oci_manifests_subject;
ALTER TABLE oci_manifests DROP COLUMN artifact_type;
ALTER TABLE oci_manifests DROP COLUMN subject;
