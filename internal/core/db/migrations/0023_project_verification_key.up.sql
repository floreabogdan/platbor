-- A project's cosign signature-verification public key (PEM). When set, signatures
-- that are not keyless (carry no certificate) are verified against it. Empty means
-- no key is configured, so only keyless signatures can be cryptographically
-- verified. This is a public key, not a secret.
ALTER TABLE projects ADD COLUMN verification_key TEXT NOT NULL DEFAULT '';
