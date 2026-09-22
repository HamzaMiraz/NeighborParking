package httpapi

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"neighborparking/internal/domain"
	"neighborparking/internal/platform"
	"neighborparking/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type registration struct {
	FullName     string `json:"fullName"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Phone        string `json:"phone"`
	VehicleName  string `json:"vehicleName"`
	LicensePlate string `json:"licensePlate"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var in registration
	if err := decode(r, &in); err != nil {
		return err
	}
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = platform.NormalizeEmail(in.Email)
	in.Phone = strings.TrimSpace(in.Phone)
	in.VehicleName = strings.TrimSpace(in.VehicleName)
	in.LicensePlate = platform.NormalizePlate(in.LicensePlate)
	if len(in.FullName) < 2 || !platform.ValidEmail(in.Email) || len(in.Password) < 8 || in.Phone == "" || in.VehicleName == "" || in.LicensePlate == "" {
		return platform.E(422, "VALIDATION_FAILED", "Provide a valid name, email, password of at least 8 characters, phone, vehicle, and license plate.")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u := domain.User{ID: platform.NewID(), FullName: in.FullName, Email: in.Email, Phone: in.Phone, VehicleName: in.VehicleName, LicensePlate: in.LicensePlate, CreatedAt: time.Now().UTC()}
	if err = s.store.CreateUser(r.Context(), u, string(hash)); err != nil {
		return err
	}
	if err = s.startSession(w, r, u.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": u})
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in credentials
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Email = platform.NormalizeEmail(in.Email)
	u, hash, err := s.store.UserByEmail(r.Context(), in.Email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		return platform.E(401, "INVALID_CREDENTIALS", "Email or password is incorrect.")
	}
	if err = s.startSession(w, r, u.ID); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token := platform.NewToken()
	if err := s.store.CreateSession(r.Context(), userID, platform.HashToken(token), time.Now().UTC().Add(s.cfg.SessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "np_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(s.cfg.SessionTTL.Seconds())})
	return nil
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie("np_session")
	if err != nil || c.Value == "" {
		return platform.E(401, "AUTHENTICATION_REQUIRED", "Please sign in.")
	}
	oldHash := platform.HashToken(c.Value)
	u, err := s.store.UserBySession(r.Context(), oldHash)
	if store.IsNotFound(err) {
		return platform.E(401, "SESSION_EXPIRED", "Your session has expired.")
	}
	if err != nil {
		return err
	}
	newToken := platform.NewToken()
	if err = s.store.RotateSession(r.Context(), oldHash, u.ID, platform.HashToken(newToken), time.Now().UTC().Add(s.cfg.SessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "np_session", Value: newToken, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: int(s.cfg.SessionTTL.Seconds())})
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie("np_session"); err == nil {
		_ = s.store.RevokeSession(r.Context(), platform.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "np_session", Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]any{"user": userFrom(r)})
	return nil
}
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		FullName     string `json:"fullName"`
		Phone        string `json:"phone"`
		VehicleName  string `json:"vehicleName"`
		LicensePlate string `json:"licensePlate"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	u.FullName = strings.TrimSpace(in.FullName)
	u.Phone = strings.TrimSpace(in.Phone)
	u.VehicleName = strings.TrimSpace(in.VehicleName)
	u.LicensePlate = platform.NormalizePlate(in.LicensePlate)
	if len(u.FullName) < 2 || u.Phone == "" || u.VehicleName == "" || u.LicensePlate == "" {
		return platform.E(422, "VALIDATION_FAILED", "All profile fields are required.")
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"user": u})
	return nil
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.Dashboard(r.Context(), userFrom(r).ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"communities": items})
	return nil
}
func (s *Server) searchCommunities(w http.ResponseWriter, r *http.Request) error {
	items, err := s.store.SearchCommunities(r.Context(), userFrom(r).ID, r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"communities": items})
	return nil
}

func (s *Server) createCommunity(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Address     string `json:"address"`
		GridRows    int    `json:"gridRows"`
		GridCols    int    `json:"gridCols"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) < 2 || in.GridRows < 1 || in.GridRows > 50 || in.GridCols < 1 || in.GridCols > 50 {
		return platform.E(422, "VALIDATION_FAILED", "Name and grid dimensions between 1 and 50 are required.")
	}
	c := domain.Community{ID: platform.NewID(), Name: in.Name, Code: store.CommunityCode(), Description: strings.TrimSpace(in.Description), Address: strings.TrimSpace(in.Address), GridRows: in.GridRows, GridCols: in.GridCols, OwnerUserID: u.ID, LayoutVersion: 1, Role: domain.RoleOwner, CreatedAt: time.Now().UTC()}
	if err := s.store.CreateCommunity(r.Context(), c, u.ID); err != nil {
		return err
	}
	s.auditEvent("COMMUNITY_CREATED", u.ID, &c.ID, nil, nil, nil, map[string]any{"name": c.Name})
	writeJSON(w, 201, map[string]any{"community": c})
	return nil
}
func (s *Server) community(w http.ResponseWriter, r *http.Request) error {
	c, err := s.store.CommunityByID(r.Context(), r.PathValue("communityID"), userFrom(r).ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"community": c})
	return nil
}
func (s *Server) communityMap(w http.ResponseWriter, r *http.Request) error {
	c, err := s.store.Map(r.Context(), r.PathValue("communityID"), userFrom(r).ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"community": c})
	return nil
}

func (s *Server) requestJoin(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if err := s.store.RequestJoin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	s.auditEvent("JOIN_REQUESTED", u.ID, &cid, &u.ID, nil, nil, nil)
	writeJSON(w, 201, map[string]any{"status": "PENDING"})
	return nil
}
func (s *Server) joinRequests(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	items, err := s.store.JoinRequests(r.Context(), cid)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"requests": items})
	return nil
}
func (s *Server) decideJoin(w http.ResponseWriter, r *http.Request, decision string) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	mid := r.PathValue("membershipID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	if err := s.store.DecideJoin(r.Context(), cid, mid, u.ID, decision); err != nil {
		return err
	}
	s.auditEvent("JOIN_REQUEST_"+decision, u.ID, &cid, nil, nil, nil, map[string]any{"membershipId": mid})
	writeJSON(w, 200, map[string]any{"status": decision})
	return nil
}
func (s *Server) approveJoin(w http.ResponseWriter, r *http.Request) error {
	return s.decideJoin(w, r, "APPROVED")
}
func (s *Server) rejectJoin(w http.ResponseWriter, r *http.Request) error {
	return s.decideJoin(w, r, "REJECTED")
}

func (s *Server) members(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	role, err := s.requireMember(r.Context(), cid, u.ID)
	if err != nil {
		return err
	}
	items, err := s.store.Members(r.Context(), cid)
	if err != nil {
		return err
	}
	if !domain.IsAdmin(role) {
		for i := range items {
			items[i].User.Email = ""
			items[i].User.Phone = ""
			items[i].User.LicensePlate = ""
		}
	}
	writeJSON(w, 200, map[string]any{"members": items, "viewerRole": role})
	return nil
}
func (s *Server) leaveCommunity(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if err := s.store.Leave(r.Context(), cid, u.ID); err != nil {
		return err
	}
	s.hub.Revoke(cid, u.ID)
	s.auditEvent("MEMBER_LEFT", u.ID, &cid, &u.ID, nil, nil, nil)
	w.WriteHeader(204)
	return nil
}

func findMember(items []domain.Membership, id string) (domain.Membership, bool) {
	for _, m := range items {
		if m.ID == id {
			return m, true
		}
	}
	return domain.Membership{}, false
}
func (s *Server) removeOrBan(w http.ResponseWriter, r *http.Request, status string) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	mid := r.PathValue("membershipID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	items, err := s.store.Members(r.Context(), cid)
	if err != nil {
		return err
	}
	target, ok := findMember(items, mid)
	if !ok && status == "REMOVED" {
		return platform.E(404, "MEMBER_NOT_FOUND", "Member not found.")
	}
	slot, err := s.store.RemoveMember(r.Context(), cid, mid, u.ID, status)
	if err != nil {
		return err
	}
	if ok {
		s.hub.Revoke(cid, target.UserID)
	}
	s.auditEvent("MEMBER_"+status, u.ID, &cid, func() *string {
		if ok {
			return &target.UserID
		}
		return nil
	}(), func() *string {
		if slot != "" {
			return &slot
		}
		return nil
	}(), nil, nil)
	if slot != "" {
		s.publishSlot(cid, slot, "SLOT_RELEASED", "AVAILABLE")
	}
	w.WriteHeader(204)
	return nil
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) error {
	return s.removeOrBan(w, r, "REMOVED")
}
func (s *Server) banMember(w http.ResponseWriter, r *http.Request) error {
	return s.removeOrBan(w, r, "BANNED")
}
func (s *Server) unbanMember(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	if err := s.store.Unban(r.Context(), cid, r.PathValue("membershipID")); err != nil {
		return err
	}
	s.auditEvent("MEMBER_UNBANNED", u.ID, &cid, nil, nil, nil, map[string]any{"membershipId": r.PathValue("membershipID")})
	w.WriteHeader(204)
	return nil
}

func (s *Server) changeRole(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	var in struct {
		Role string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Role = strings.ToUpper(in.Role)
	if err := s.store.ChangeRole(r.Context(), cid, r.PathValue("membershipID"), in.Role); err != nil {
		return err
	}
	s.auditEvent("ROLE_CHANGED", u.ID, &cid, nil, nil, nil, map[string]any{"membershipId": r.PathValue("membershipID"), "role": in.Role})
	writeJSON(w, 200, map[string]any{"role": in.Role})
	return nil
}
func (s *Server) transferOwnership(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if err := s.requireOwner(r.Context(), cid, u.ID); err != nil {
		return err
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.UserID == u.ID {
		return platform.E(409, "ALREADY_OWNER", "You already own this community.")
	}
	if err := s.store.TransferOwnership(r.Context(), cid, u.ID, in.UserID); err != nil {
		return err
	}
	s.auditEvent("OWNERSHIP_TRANSFERRED", u.ID, &cid, &in.UserID, nil, nil, nil)
	writeJSON(w, 200, map[string]any{"ownerUserId": in.UserID})
	return nil
}

func (s *Server) saveLayout(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	var in domain.LayoutInput
	if err := decode(r, &in); err != nil {
		return err
	}
	version, err := s.store.SaveLayout(r.Context(), cid, in)
	if err != nil {
		return err
	}
	s.auditEvent("LAYOUT_UPDATED", u.ID, &cid, nil, nil, nil, map[string]any{"version": version})
	s.hub.Publish(domain.RealtimeEvent{ID: platform.NewID(), Type: "LAYOUT_UPDATED", CommunityID: cid, Version: version, Timestamp: time.Now().UTC()})
	writeJSON(w, 200, map[string]any{"layoutVersion": version})
	return nil
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireMember(r.Context(), cid, u.ID); err != nil {
		return err
	}
	return s.doCheckIn(w, r, u.ID, u.ID, "SELF")
}
func (s *Server) forceCheckIn(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	var in struct {
		UserID string `json:"userId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.UserID == "" {
		return platform.E(422, "USER_REQUIRED", "Select a member.")
	}
	return s.doCheckIn(w, r, in.UserID, u.ID, "ADMIN_FORCE")
}
func (s *Server) doCheckIn(w http.ResponseWriter, r *http.Request, target, actor, kind string) error {
	cid, slot := r.PathValue("communityID"), r.PathValue("slotID")
	unlock := s.locks.Lock(slot)
	defer unlock()
	result, err := s.store.CheckIn(r.Context(), cid, slot, target, actor, kind)
	if err != nil {
		return err
	}
	s.auditEvent("CHECK_IN_"+kind, actor, &cid, &target, &slot, &result.OccupancyID, nil)
	s.publishSlot(cid, slot, "SLOT_OCCUPIED", "OCCUPIED")
	writeJSON(w, 201, map[string]any{"occupancy": result})
	return nil
}
func (s *Server) checkOut(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireMember(r.Context(), cid, u.ID); err != nil {
		return err
	}
	return s.doCheckOut(w, r, u.ID, false)
}
func (s *Server) forceCheckOut(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireAdmin(r.Context(), cid, u.ID); err != nil {
		return err
	}
	return s.doCheckOut(w, r, u.ID, true)
}
func (s *Server) doCheckOut(w http.ResponseWriter, r *http.Request, actor string, force bool) error {
	cid, slot := r.PathValue("communityID"), r.PathValue("slotID")
	unlock := s.locks.Lock(slot)
	defer unlock()
	result, err := s.store.CheckOut(r.Context(), cid, slot, actor, force)
	if err != nil {
		return err
	}
	action := "CHECK_OUT_SELF"
	if force {
		action = "CHECK_OUT_ADMIN_FORCE"
	}
	s.auditEvent(action, actor, &cid, &result.UserID, &slot, &result.OccupancyID, nil)
	s.publishSlot(cid, slot, "SLOT_RELEASED", "AVAILABLE")
	writeJSON(w, 200, map[string]any{"occupancy": result})
	return nil
}
func (s *Server) publishSlot(cid, slot, event, status string) {
	s.hub.Publish(domain.RealtimeEvent{ID: platform.NewID(), Type: event, CommunityID: cid, SlotID: slot, Data: map[string]any{"status": status}, Timestamp: time.Now().UTC()})
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) error {
	u := userFrom(r)
	cid := r.PathValue("communityID")
	if _, err := s.requireMember(r.Context(), cid, u.ID); err != nil {
		return err
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != s.cfg.AllowedOrigin {
		return platform.E(403, "ORIGIN_DENIED", "WebSocket origin is not allowed.")
	}
	s.hub.Serve(w, r, u.ID, cid)
	return nil
}

var _ = sql.ErrNoRows
