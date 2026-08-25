package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/operations"
	"github.com/11DingKing/lushan-youstay-allocation/internal/settlement"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

type Dependencies struct {
	Auth       *auth.Service
	Booking    *booking.Service
	Operations *operations.Service
	Settlement *settlement.Service
	Store      *storesqlite.Store
	Logger     *slog.Logger
}

func New(deps Dependencies) http.Handler {
	api := &API{auth: deps.Auth, booking: deps.Booking, operations: deps.Operations, settlement: deps.Settlement, store: deps.Store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", api.live)
	mux.HandleFunc("GET /health/ready", api.ready)
	mux.HandleFunc("POST /v1/auth/login", api.login)
	protected := http.NewServeMux()
	protected.HandleFunc("POST /v1/auth/logout", api.logout)
	protected.Handle("GET /v1/resources", requireRoles(auth.RoleGuest, auth.RoleFrontdesk, auth.RoleCampGuard, auth.RoleOperator)(http.HandlerFunc(api.listResources)))
	protected.Handle("POST /v1/stays/hold", requireRoles(auth.RoleGuest, auth.RoleFrontdesk, auth.RoleOperator)(http.HandlerFunc(api.hold)))
	protected.Handle("POST /v1/stays/{id}/guarantee", requireRoles(auth.RoleFrontdesk, auth.RoleOperator)(http.HandlerFunc(api.guarantee)))
	protected.Handle("POST /v1/stays/{id}/check-in", requireRoles(auth.RoleFrontdesk, auth.RoleCampGuard, auth.RoleOperator)(http.HandlerFunc(api.checkIn)))
	protected.Handle("POST /v1/stays/{id}/checkout", requireRoles(auth.RoleFrontdesk, auth.RoleCampGuard, auth.RoleOperator)(http.HandlerFunc(api.checkout)))
	protected.Handle("POST /v1/stays/{id}/settlements", requireRoles(auth.RoleFrontdesk, auth.RoleOperator)(http.HandlerFunc(api.startSettlement)))
	mux.Handle("/v1/", authenticate(deps.Auth, protected))
	return requestMiddleware(deps.Logger, mux)
}

type API struct {
	auth       *auth.Service
	booking    *booking.Service
	operations *operations.Service
	settlement *settlement.Service
	store      *storesqlite.Store
}

func (a *API) live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "live"})
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1500_000_000)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "database": "available"})
}
