package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"neighborparking/internal/domain"
	"neighborparking/internal/platform"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct{ db *sql.DB }

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Store{db: db}, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) CreateUser(ctx context.Context, u domain.User, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
      (id, full_name, email, password_hash, phone, vehicle_name, license_plate)
      VALUES (?, ?, ?, ?, ?, ?, ?)`, u.ID, u.FullName, u.Email, passwordHash, u.Phone, u.VehicleName, u.LicensePlate)
	if duplicate(err) {
		return platform.E(409, "EMAIL_EXISTS", "An account with that email already exists.")
	}
	return err
}

func scanUser(row interface{ Scan(...any) error }) (domain.User, string, error) {
	var u domain.User
	var hash string
	err := row.Scan(&u.ID, &u.FullName, &u.Email, &hash, &u.Phone, &u.VehicleName, &u.LicensePlate, &u.CreatedAt)
	return u, hash, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (domain.User, string, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, full_name, email, password_hash, phone, vehicle_name, license_plate, created_at
      FROM users WHERE email = ?`, email))
}

func (s *Store) UserByID(ctx context.Context, id string) (domain.User, error) {
	u, _, err := scanUser(s.db.QueryRowContext(ctx, `SELECT id, full_name, email, password_hash, phone, vehicle_name, license_plate, created_at
      FROM users WHERE id = ?`, id))
	return u, err
}

func (s *Store) UpdateUser(ctx context.Context, u domain.User) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET full_name=?, phone=?, vehicle_name=?, license_plate=? WHERE id=?`,
		u.FullName, u.Phone, u.VehicleName, u.LicensePlate, u.ID)
	return err
}

func (s *Store) CreateSession(ctx context.Context, userID, hash string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,user_id,token_hash,expires_at) VALUES (?,?,?,?)`,
		platform.NewID(), userID, hash, expires)
	return err
}

func (s *Store) UserBySession(ctx context.Context, hash string) (domain.User, error) {
	u, _, err := scanUser(s.db.QueryRowContext(ctx, `SELECT u.id,u.full_name,u.email,u.password_hash,u.phone,u.vehicle_name,u.license_plate,u.created_at
      FROM sessions s JOIN users u ON u.id=s.user_id
      WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at > UTC_TIMESTAMP(6)`, hash))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_used_at=UTC_TIMESTAMP(6) WHERE token_hash=?`, hash)
	}
	return u, err
}

func (s *Store) RevokeSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE token_hash=? AND revoked_at IS NULL`, hash)
	return err
}

func (s *Store) RotateSession(ctx context.Context, oldHash, userID, newHash string, expires time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE token_hash=? AND user_id=? AND revoked_at IS NULL AND expires_at>UTC_TIMESTAMP(6)`, oldHash, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return platform.E(401, "SESSION_EXPIRED", "Your session has expired.")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sessions (id,user_id,token_hash,expires_at) VALUES (?,?,?,?)`, platform.NewID(), userID, newHash, expires); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateCommunity(ctx context.Context, c domain.Community, creatorID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO communities
      (id,name,code,description,address,owner_user_id,grid_rows,grid_cols,layout_json)
      VALUES (?,?,?,?,?,?,?,?,?)`, c.ID, c.Name, c.Code, nullString(c.Description), nullString(c.Address), creatorID, c.GridRows, c.GridCols, []byte(`{"cells":[]}`)); err != nil {
		if duplicate(err) {
			return platform.E(409, "COMMUNITY_CODE_EXISTS", "Please retry community creation.")
		}
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO community_memberships
      (id,community_id,user_id,role,status,approved_at,approved_by_user_id)
      VALUES (?,?,?,'OWNER','APPROVED',UTC_TIMESTAMP(6),?)`, platform.NewID(), c.ID, creatorID, creatorID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Dashboard(ctx context.Context, userID string) ([]domain.CommunityCard, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.code,COALESCE(c.description,''),m.role,
      COALESCE(SUM(ps.status='AVAILABLE'),0),COUNT(ps.id)
      FROM community_memberships m JOIN communities c ON c.id=m.community_id
		LEFT JOIN parking_slots ps ON ps.community_id=c.id AND ps.archived_at IS NULL
      WHERE m.user_id=? AND m.status='APPROVED' AND c.status='ACTIVE'
      GROUP BY c.id,c.name,c.code,c.description,m.role ORDER BY c.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CommunityCard{}
	for rows.Next() {
		var x domain.CommunityCard
		if err := rows.Scan(&x.ID, &x.Name, &x.Code, &x.Description, &x.Role, &x.AvailableSpots, &x.TotalSpots); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}

type SearchResult struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Code             string `json:"code"`
	Description      string `json:"description,omitempty"`
	MembershipStatus string `json:"membershipStatus,omitempty"`
}

func (s *Store) SearchCommunities(ctx context.Context, userID, q string) ([]SearchResult, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []SearchResult{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.code,COALESCE(c.description,''),COALESCE(m.status,'')
      FROM communities c LEFT JOIN community_memberships m ON m.community_id=c.id AND m.user_id=?
      WHERE c.status='ACTIVE' AND (c.code=? OR c.name LIKE ?) ORDER BY (c.code=?) DESC,c.name LIMIT 30`, userID, strings.ToUpper(q), "%"+q+"%", strings.ToUpper(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SearchResult{}
	for rows.Next() {
		var x SearchResult
		if err := rows.Scan(&x.ID, &x.Name, &x.Code, &x.Description, &x.MembershipStatus); err != nil {
			return nil, err
		}
		result = append(result, x)
	}
	return result, rows.Err()
}

func (s *Store) MembershipRole(ctx context.Context, communityID, userID string) (role, status string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT role,status FROM community_memberships WHERE community_id=? AND user_id=?`, communityID, userID).Scan(&role, &status)
	return
}

func (s *Store) RequestJoin(ctx context.Context, communityID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM community_memberships WHERE community_id=? AND user_id=? FOR UPDATE`, communityID, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO community_memberships (id,community_id,user_id,role,status) VALUES (?,?,?,'MEMBER','PENDING')`, platform.NewID(), communityID, userID)
	} else if err == nil {
		switch status {
		case "BANNED":
			return platform.E(403, "MEMBERSHIP_BANNED", "You cannot request access to this community.")
		case "PENDING":
			return platform.E(409, "REQUEST_ALREADY_PENDING", "A join request is already pending.")
		case "APPROVED":
			return platform.E(409, "ALREADY_MEMBER", "You are already a member.")
		}
		_, err = tx.ExecContext(ctx, `UPDATE community_memberships SET role='MEMBER',status='PENDING',requested_at=UTC_TIMESTAMP(6),approved_at=NULL,approved_by_user_id=NULL,status_reason=NULL WHERE community_id=? AND user_id=?`, communityID, userID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) JoinRequests(ctx context.Context, communityID string) ([]domain.Membership, error) {
	return s.members(ctx, communityID, "PENDING")
}

func (s *Store) Members(ctx context.Context, communityID string) ([]domain.Membership, error) {
	return s.members(ctx, communityID, "APPROVED")
}

func (s *Store) members(ctx context.Context, communityID, status string) ([]domain.Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.community_id,m.user_id,m.role,m.status,m.requested_at,m.approved_at,
      u.id,u.full_name,u.email,u.phone,u.vehicle_name,u.license_plate,u.created_at
      FROM community_memberships m JOIN users u ON u.id=m.user_id WHERE m.community_id=? AND m.status=? ORDER BY u.full_name`, communityID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Membership{}
	for rows.Next() {
		var m domain.Membership
		if err := rows.Scan(&m.ID, &m.CommunityID, &m.UserID, &m.Role, &m.Status, &m.RequestedAt, &m.ApprovedAt, &m.User.ID, &m.User.FullName, &m.User.Email, &m.User.Phone, &m.User.VehicleName, &m.User.LicensePlate, &m.User.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DecideJoin(ctx context.Context, communityID, membershipID, actorID, decision string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM community_memberships WHERE id=? AND community_id=? FOR UPDATE`, membershipID, communityID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return platform.E(404, "REQUEST_NOT_FOUND", "Join request not found.")
	}
	if err != nil {
		return err
	}
	if status != "PENDING" {
		return platform.E(409, "REQUEST_ALREADY_DECIDED", "This request has already been decided.")
	}
	if decision == "APPROVED" {
		_, err = tx.ExecContext(ctx, `UPDATE community_memberships SET status='APPROVED',role='MEMBER',approved_at=UTC_TIMESTAMP(6),approved_by_user_id=? WHERE id=?`, actorID, membershipID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE community_memberships SET status='REJECTED',approved_at=NULL,approved_by_user_id=? WHERE id=?`, actorID, membershipID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ChangeRole(ctx context.Context, communityID, membershipID, role string) error {
	if role != "ADMIN" && role != "MEMBER" {
		return platform.E(400, "INVALID_ROLE", "Role must be ADMIN or MEMBER.")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE community_memberships SET role=? WHERE id=? AND community_id=? AND status='APPROVED' AND role<>'OWNER'`, role, membershipID, communityID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return platform.E(404, "MEMBER_NOT_FOUND", "Eligible member not found.")
	}
	return nil
}

func (s *Store) Unban(ctx context.Context, communityID, membershipID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE community_memberships SET status='REJECTED',status_reason=NULL WHERE id=? AND community_id=? AND status='BANNED'`, membershipID, communityID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return platform.E(404, "BANNED_MEMBER_NOT_FOUND", "Banned membership not found.")
	}
	return nil
}

func (s *Store) TransferOwnership(ctx context.Context, communityID, currentOwnerID, newOwnerID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role, status string
	if err = tx.QueryRowContext(ctx, `SELECT role,status FROM community_memberships WHERE community_id=? AND user_id=? FOR UPDATE`, communityID, newOwnerID).Scan(&role, &status); err != nil {
		return platform.E(404, "MEMBER_NOT_FOUND", "New owner must be an approved member.")
	}
	if status != "APPROVED" {
		return platform.E(409, "MEMBER_NOT_APPROVED", "New owner must be approved.")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE community_memberships SET role='ADMIN' WHERE community_id=? AND user_id=? AND role='OWNER'`, communityID, currentOwnerID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE community_memberships SET role='OWNER' WHERE community_id=? AND user_id=?`, communityID, newOwnerID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE communities SET owner_user_id=? WHERE id=? AND owner_user_id=?`, newOwnerID, communityID, currentOwnerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Leave(ctx context.Context, communityID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role, status string
	if err = tx.QueryRowContext(ctx, `SELECT role,status FROM community_memberships WHERE community_id=? AND user_id=? FOR UPDATE`, communityID, userID).Scan(&role, &status); err != nil {
		return platform.E(404, "MEMBERSHIP_NOT_FOUND", "Membership not found.")
	}
	if role == "OWNER" {
		return platform.E(409, "LAST_OWNER_CANNOT_LEAVE", "Transfer ownership before leaving.")
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM occupancies WHERE community_id=? AND user_id=? AND checked_out_at IS NULL`, communityID, userID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return platform.E(409, "ACTIVE_OCCUPANCY_EXISTS", "Check out before leaving the community.")
	}
	_, err = tx.ExecContext(ctx, `UPDATE community_memberships SET status='LEFT' WHERE community_id=? AND user_id=?`, communityID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveMember(ctx context.Context, communityID, membershipID, actorID, newStatus string) (slotID string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var userID, role, status string
	err = tx.QueryRowContext(ctx, `SELECT user_id,role,status FROM community_memberships WHERE id=? AND community_id=? FOR UPDATE`, membershipID, communityID).Scan(&userID, &role, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", platform.E(404, "MEMBER_NOT_FOUND", "Member not found.")
	}
	if err != nil {
		return "", err
	}
	if role == "OWNER" {
		return "", platform.E(409, "OWNER_CANNOT_BE_REMOVED", "Transfer ownership first.")
	}
	if status != "APPROVED" && newStatus == "REMOVED" {
		return "", platform.E(409, "MEMBER_NOT_APPROVED", "Only approved members can be removed.")
	}
	var occID string
	err = tx.QueryRowContext(ctx, `SELECT id,parking_slot_id FROM occupancies WHERE community_id=? AND user_id=? AND checked_out_at IS NULL`, communityID, userID).Scan(&occID, &slotID)
	if err == nil {
		var lockedSlot string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM parking_slots WHERE id=? FOR UPDATE`, slotID).Scan(&lockedSlot); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE occupancies SET checked_out_at=UTC_TIMESTAMP(6),checked_out_by_user_id=?,checkout_type='MEMBER_REMOVAL' WHERE id=? AND checked_out_at IS NULL`, actorID, occID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE parking_slots SET status='AVAILABLE',occupied_by_user_id=NULL,active_occupancy_id=NULL WHERE id=?`, slotID); err != nil {
			return "", err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE community_memberships SET status=? WHERE id=?`, newStatus, membershipID); err != nil {
		return "", err
	}
	return slotID, tx.Commit()
}

func (s *Store) CommunityByID(ctx context.Context, communityID, userID string) (domain.Community, error) {
	var c domain.Community
	err := s.db.QueryRowContext(ctx, `SELECT c.id,c.name,c.code,COALESCE(c.description,''),COALESCE(c.address,''),c.owner_user_id,c.grid_rows,c.grid_cols,c.layout_json,c.layout_version,c.created_at,m.role
      FROM communities c JOIN community_memberships m ON m.community_id=c.id AND m.user_id=? AND m.status='APPROVED'
      WHERE c.id=? AND c.status='ACTIVE'`, userID, communityID).Scan(&c.ID, &c.Name, &c.Code, &c.Description, &c.Address, &c.OwnerUserID, &c.GridRows, &c.GridCols, &c.LayoutJSON, &c.LayoutVersion, &c.CreatedAt, &c.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return c, platform.E(404, "COMMUNITY_NOT_FOUND", "Community not found or access denied.")
	}
	return c, err
}

func (s *Store) InsertAudit(ctx context.Context, e domain.AuditEvent) error {
	meta, _ := json.Marshal(e.Metadata)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs (id,community_id,actor_user_id,action_type,target_user_id,target_slot_id,target_occupancy_id,metadata_json) VALUES (?,?,?,?,?,?,?,?)`, e.ID, e.CommunityID, e.ActorUserID, e.ActionType, e.TargetUserID, e.TargetSlotID, e.TargetOccupancyID, nullBytes(meta))
	return err
}

func duplicate(err error) bool { return err != nil && strings.Contains(err.Error(), "Duplicate entry") }
func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
func nullBytes(v []byte) any {
	if len(v) == 0 || string(v) == "null" {
		return nil
	}
	return v
}

func CommunityCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		panic("secure random source unavailable: " + err.Error())
	}
	out := make([]byte, 8)
	for i := range out {
		out[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(out)
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
func WrapDB(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
