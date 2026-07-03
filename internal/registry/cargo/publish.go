package cargo

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// maxCrateSize caps a single published .crate.
const maxCrateSize = 1 << 30 // 1 GiB

// maxPublishBody caps the whole publish request body.
const maxPublishBody = maxCrateSize + (16 << 20)

// publish handles `cargo publish`: PUT /api/v1/crates/new with a binary body of
//
//	<u32 LE json-len><json metadata><u32 LE crate-len><.crate bytes>
//
// The .crate is stored in the blob store; the index line is built from the
// metadata plus the crate's sha256. Proxy repos are read-only; a re-publish is
// a 409.
func (h *handler) publish(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, true)
	if !ok {
		return
	}
	if repo.Mode == repository.ModeProxy {
		writeError(w, http.StatusForbidden, "cannot publish to a proxy repository")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxPublishBody))
	if err != nil {
		h.internalError(w, "reading publish body", err)
		return
	}
	metaBytes, crateBytes, err := splitPublishBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid publish body: "+err.Error())
		return
	}
	var meta publishMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		writeError(w, http.StatusBadRequest, "invalid crate metadata: "+err.Error())
		return
	}
	if meta.Name == "" || meta.Vers == "" {
		writeError(w, http.StatusBadRequest, "missing crate name or version")
		return
	}

	sum := sha256.Sum256(crateBytes)
	cksum := hex.EncodeToString(sum[:])
	indexLine, err := buildIndexLine(meta, cksum)
	if err != nil {
		h.internalError(w, "building index line", err)
		return
	}

	digest, size, err := h.storeBlob(r, crateBytes)
	if err != nil {
		h.internalError(w, "storing crate", err)
		return
	}

	if err := h.store.publish(r.Context(), publishInput{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		Name:         meta.Name,
		Version:      meta.Vers,
		IndexLine:    indexLine,
		Cksum:        cksum,
		BlobDigest:   digest,
		Size:         size,
		Actor:        actorFrom(r),
	}); err != nil {
		if errors.Is(err, ErrVersionExists) {
			writeError(w, http.StatusConflict, fmt.Sprintf("crate version %s@%s already exists", meta.Name, meta.Vers))
			return
		}
		h.internalError(w, "recording crate", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"warnings": map[string]any{"invalid_categories": []string{}, "invalid_badges": []string{}, "other": []string{}},
	})
}

// splitPublishBody parses the length-prefixed publish body into its metadata and
// .crate parts.
func splitPublishBody(body []byte) (meta, crate []byte, err error) {
	if len(body) < 4 {
		return nil, nil, errors.New("truncated")
	}
	jsonLen := binary.LittleEndian.Uint32(body[:4])
	off := 4 + int(jsonLen)
	if off+4 > len(body) {
		return nil, nil, errors.New("metadata length out of range")
	}
	meta = body[4:off]
	crateLen := binary.LittleEndian.Uint32(body[off : off+4])
	start := off + 4
	if start+int(crateLen) > len(body) {
		return nil, nil, errors.New("crate length out of range")
	}
	crate = body[start : start+int(crateLen)]
	return meta, crate, nil
}

// storeBlob commits bytes to the content-addressable store.
func (h *handler) storeBlob(r *http.Request, data []byte) (string, int64, error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, err
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}
