package npm

import (
	"encoding/json"
	"net/http"
)

// loginRequest is the CouchDB user document npm's legacy login PUTs to
// /-/user/org.couchdb.user:<name>. Only name and password matter to us.
type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// loginResponse mirrors what npm expects back from the user-document PUT: an ok
// flag, the document id, and a bearer token it will store in .npmrc.
type loginResponse struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

// login handles `npm login` (and `npm adduser`) in its legacy mode: authenticate
// the username/password, then issue a personal access token as the bearer the
// client will use for subsequent publishes and installs.
func (h *handler) login(w http.ResponseWriter, r *http.Request, urlUser string) {
	if r.Method != http.MethodPut {
		writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, h.log, http.StatusBadRequest, "invalid request body")
		return
	}
	name := req.Name
	if name == "" {
		name = urlUser
	}

	user, err := h.auth.Authenticate(r.Context(), name, req.Password)
	if err != nil {
		writeError(w, h.log, http.StatusUnauthorized, "incorrect username or password")
		return
	}

	raw, _, err := h.auth.CreateToken(r.Context(), user.ID, user.Username, "npm login", 0)
	if err != nil {
		h.internalError(w, "issuing npm token", err)
		return
	}

	writeJSON(w, h.log, http.StatusCreated, loginResponse{
		OK:    true,
		ID:    "org.couchdb.user:" + user.Username,
		Token: raw,
	})
}
