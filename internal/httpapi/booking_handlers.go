package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/observability"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
)

func (a *API) listResources(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	filter := storesqlite.ResourceFilter{PropertyID: r.URL.Query().Get("property_id"), Kind: domain.ResourceKind(r.URL.Query().Get("kind")), Status: domain.ResourceStatus(r.URL.Query().Get("status")), Limit: limit, Offset: offset}
	items, total, err := a.store.ListResources(r.Context(), filter)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (a *API) hold(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var body struct {
		GuestID        string `json:"guest_id"`
		ResourceID     string `json:"resource_id"`
		CheckIn        string `json:"check_in"`
		CheckOut       string `json:"check_out"`
		PartySize      int    `json:"party_size"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	guestID := body.GuestID
	if guestID == "" {
		guestID = p.UserID
	}
	stay, err := a.booking.Hold(r.Context(), booking.HoldRequest{GuestID: guestID, ResourceID: body.ResourceID, CheckInDate: body.CheckIn, CheckOutDate: body.CheckOut, PartySize: body.PartySize, IdempotencyKey: body.IdempotencyKey, RequestID: observability.RequestID(r.Context())})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, stay)
}

func (a *API) guarantee(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body struct {
		GuestID         string `json:"guest_id"`
		DocumentDigest  string `json:"document_digest"`
		Provider        string `json:"provider"`
		ProviderEventID string `json:"provider_event_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	stay, err := a.booking.Guarantee(r.Context(), booking.GuaranteeRequest{StayID: r.PathValue("id"), GuestID: body.GuestID, DocumentDigest: body.DocumentDigest, Provider: body.Provider, ProviderEventID: body.ProviderEventID, RequestID: observability.RequestID(r.Context()), VerifiedBy: p.UserID})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stay)
}

func (a *API) checkIn(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	stay, err := a.booking.CheckIn(r.Context(), r.PathValue("id"), p.UserID, observability.RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stay)
}

func (a *API) checkout(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	task, err := a.operations.Checkout(r.Context(), r.PathValue("id"), p.UserID, observability.RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func (a *API) startSettlement(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	result, err := a.settlement.Start(r.Context(), r.PathValue("id"), p.UserID, observability.RequestID(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func parseRFC3339(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, domain.ErrInvalid
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, domain.ErrInvalid
	}
	return parsed, nil
}
