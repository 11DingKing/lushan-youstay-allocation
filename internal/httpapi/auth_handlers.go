package httpapi

import (
	"net/http"
	"strings"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
)

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	token, principal, err := a.auth.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": map[string]any{"id": principal.UserID, "display_name": principal.DisplayName, "role": principal.Role}})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if err := a.auth.Logout(r.Context(), token); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func principal(r *http.Request) (auth.Principal, error) {
	value, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		return auth.Principal{}, domain.ErrUnauthorized
	}
	return value, nil
}
