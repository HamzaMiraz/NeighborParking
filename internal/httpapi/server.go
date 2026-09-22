package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"neighborparking/internal/audit"
	"neighborparking/internal/config"
	"neighborparking/internal/domain"
	"neighborparking/internal/parking"
	"neighborparking/internal/platform"
	"neighborparking/internal/realtime"
	"neighborparking/internal/store"
)

type contextKey string

const userKey contextKey = "user"

type Server struct {
	cfg     config.Config
	store   *store.Store
	hub     *realtime.Hub
	audit   *audit.Queue
	locks   *parking.LockManager
	limiter *rateLimiter
	log     *slog.Logger
	mux     *http.ServeMux
}

func New(cfg config.Config, st *store.Store, hub *realtime.Hub, aq *audit.Queue, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, hub: hub, audit: aq, locks: parking.NewLockManager(), limiter: newRateLimiter(), log: log, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.recover(s.security(s.requestLog(s.mux))) }

type handler func(http.ResponseWriter, *http.Request) error

func (s *Server) handle(pattern string, h handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}
func (s *Server) protected(pattern string, h handler) {
	s.handle(pattern, func(w http.ResponseWriter, r *http.Request) error {
		u, err := s.authenticate(r)
		if err != nil {
			return err
		}
		return h(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (s *Server) routes() {
	s.handle("GET /api/v1/health", s.health)
	s.limited("POST /api/v1/auth/register", 10, time.Minute, s.register)
	s.limited("POST /api/v1/auth/login", 20, time.Minute, s.login)
	s.limited("POST /api/v1/auth/refresh", 30, time.Minute, s.refresh)
	s.handle("POST /api/v1/auth/logout", s.logout)
	s.protected("GET /api/v1/me", s.me)
	s.protected("PATCH /api/v1/me", s.updateMe)
	s.protected("GET /api/v1/dashboard/communities", s.dashboard)
	s.protected("GET /api/v1/communities/search", s.searchCommunities)
	s.protected("POST /api/v1/communities", s.createCommunity)
	s.protected("GET /api/v1/communities/{communityID}", s.community)
	s.protected("GET /api/v1/communities/{communityID}/map", s.communityMap)
	s.protected("GET /api/v1/communities/{communityID}/members", s.members)
	s.protected("POST /api/v1/communities/{communityID}/join-requests", s.requestJoin)
	s.protected("GET /api/v1/communities/{communityID}/join-requests", s.joinRequests)
	s.protected("POST /api/v1/communities/{communityID}/join-requests/{membershipID}/approve", s.approveJoin)
	s.protected("POST /api/v1/communities/{communityID}/join-requests/{membershipID}/reject", s.rejectJoin)
	s.protected("POST /api/v1/communities/{communityID}/leave", s.leaveCommunity)
	s.protected("POST /api/v1/communities/{communityID}/members/{membershipID}/remove", s.removeMember)
	s.protected("POST /api/v1/communities/{communityID}/members/{membershipID}/ban", s.banMember)
	s.protected("POST /api/v1/communities/{communityID}/members/{membershipID}/unban", s.unbanMember)
	s.protected("PATCH /api/v1/communities/{communityID}/members/{membershipID}/role", s.changeRole)
	s.protected("POST /api/v1/communities/{communityID}/ownership/transfer", s.transferOwnership)
	s.protected("PUT /api/v1/communities/{communityID}/layout", s.saveLayout)
	s.protected("POST /api/v1/communities/{communityID}/slots/{slotID}/check-in", s.checkIn)
	s.protected("POST /api/v1/communities/{communityID}/slots/{slotID}/check-out", s.checkOut)
	s.protected("POST /api/v1/communities/{communityID}/slots/{slotID}/force-check-in", s.forceCheckIn)
	s.protected("POST /api/v1/communities/{communityID}/slots/{slotID}/force-check-out", s.forceCheckOut)
	s.protected("GET /api/v1/ws/communities/{communityID}", s.websocket)
	s.serveFrontend()
}

func userFrom(r *http.Request) domain.User { return r.Context().Value(userKey).(domain.User) }

func (s *Server) authenticate(r *http.Request) (domain.User, error) {
	c, err := r.Cookie("np_session")
	if err != nil || c.Value == "" {
		return domain.User{}, platform.E(401, "AUTHENTICATION_REQUIRED", "Please sign in.")
	}
	u, err := s.store.UserBySession(r.Context(), platform.HashToken(c.Value))
	if store.IsNotFound(err) {
		return u, platform.E(401, "SESSION_EXPIRED", "Your session has expired.")
	}
	if err != nil {
		return u, err
	}
	return u, nil
}

func (s *Server) requireMember(ctx context.Context, communityID, userID string) (string, error) {
	role, status, err := s.store.MembershipRole(ctx, communityID, userID)
	if err != nil || status != domain.StatusApproved {
		return "", platform.E(403, "MEMBERSHIP_REQUIRED", "Approved community membership is required.")
	}
	return role, nil
}
func (s *Server) requireAdmin(ctx context.Context, communityID, userID string) (string, error) {
	role, err := s.requireMember(ctx, communityID, userID)
	if err != nil {
		return "", err
	}
	if !domain.IsAdmin(role) {
		return "", platform.E(403, "ADMIN_REQUIRED", "Community administrator access is required.")
	}
	return role, nil
}
func (s *Server) requireOwner(ctx context.Context, communityID, userID string) error {
	role, err := s.requireMember(ctx, communityID, userID)
	if err != nil {
		return err
	}
	if role != domain.RoleOwner {
		return platform.E(403, "OWNER_REQUIRED", "Community owner access is required.")
	}
	return nil
}

func decode(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nilWriter{}, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return platform.E(400, "INVALID_JSON", "Request body is invalid.", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return platform.E(400, "INVALID_JSON", "Request body must contain one JSON object.")
	}
	return nil
}

type nilWriter struct{}

func (nilWriter) Header() http.Header       { return make(http.Header) }
func (nilWriter) Write([]byte) (int, error) { return 0, nil }
func (nilWriter) WriteHeader(int)           {}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	ae := platform.AsAppError(err)
	if ae.Status >= 500 {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeJSON(w, ae.Status, map[string]any{"error": map[string]any{"code": ae.Code, "message": ae.Message}})
}

func (s *Server) auditEvent(action, actor string, community, targetUser, targetSlot, targetOcc *string, meta map[string]any) {
	s.audit.Enqueue(domain.AuditEvent{ID: platform.NewID(), CommunityID: community, ActorUserID: actor, ActionType: action, TargetUserID: targetUser, TargetSlotID: targetSlot, TargetOccupancyID: targetOcc, Metadata: meta})
}
func ptr(v string) *string { return &v }

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		return platform.E(503, "DATABASE_UNAVAILABLE", "Database is unavailable.", err)
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "time": time.Now().UTC()})
	return nil
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != s.cfg.AllowedOrigin {
				s.writeError(w, r, platform.E(403, "ORIGIN_DENIED", "Request origin is not allowed."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic recovered", "value", v)
				s.writeError(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}

func (s *Server) serveFrontend() {
	assets, err := fs.Sub(frontend, "web")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(assets))
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", files))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		b, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.Error(w, "frontend unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
}
