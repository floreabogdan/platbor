-- Referrers (OCI Distribution Spec v1.1): a manifest may carry a `subject`
-- descriptor pointing at another manifest by digest — this is how a signature,
-- SBOM, or attestation says "I refer to that image". We denormalize the subject
-- digest and the artifact type out of the payload so the referrers API can list
-- a subject's referrers with an index lookup instead of scanning every manifest.
ALTER TABLE oci_manifests ADD COLUMN subject TEXT NOT NULL DEFAULT '';
ALTER TABLE oci_manifests ADD COLUMN artifact_type TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_oci_manifests_subject ON oci_manifests (project_id, repository, subject);
