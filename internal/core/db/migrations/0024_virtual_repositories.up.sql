-- Virtual (group) repositories aggregate several member repositories behind one
-- URL. A read against the virtual repository is resolved against its members in
-- order (first hit wins); writes are rejected (a virtual repository is a view,
-- not a store). Members are ordinary local or proxy OCI repositories in the same
-- project; virtual repositories do not nest.
--
-- mode = 'virtual' on the repositories row marks the aggregate; this table holds
-- its ordered members.
CREATE TABLE virtual_repo_members (
    virtual_repo_id TEXT    NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    member_repo_id  TEXT    NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    PRIMARY KEY (virtual_repo_id, member_repo_id)
);

CREATE INDEX idx_vrm_order ON virtual_repo_members (virtual_repo_id, position);
