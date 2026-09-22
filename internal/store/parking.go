package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"neighborparking/internal/domain"
	"neighborparking/internal/platform"
)

func (s *Store) Map(ctx context.Context, communityID, viewerID string) (domain.Community, error) {
	c, err := s.CommunityByID(ctx, communityID, viewerID)
	if err != nil {
		return c, err
	}
	var static struct {
		Cells []domain.MapCell `json:"cells"`
	}
	if len(c.LayoutJSON) > 0 {
		_ = json.Unmarshal(c.LayoutJSON, &static)
	}
	c.Cells = static.Cells
	rows, err := s.db.QueryContext(ctx, `SELECT ps.id,ps.slot_name,ps.row_idx,ps.col_idx,ps.status,
      COALESCE(u.id,''),COALESCE(u.full_name,''),COALESCE(u.vehicle_name,''),COALESCE(u.license_plate,''),COALESCE(u.email,''),COALESCE(u.phone,''),o.checked_in_at
      FROM parking_slots ps
      LEFT JOIN users u ON u.id=ps.occupied_by_user_id
      LEFT JOIN occupancies o ON o.id=ps.active_occupancy_id
		WHERE ps.community_id=? AND ps.archived_at IS NULL`, communityID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var cell domain.MapCell
		var uid, name, vehicle, plate, email, phone string
		var checked sql.NullTime
		if err := rows.Scan(&cell.SlotID, &cell.Label, &cell.Row, &cell.Col, &cell.Status, &uid, &name, &vehicle, &plate, &email, &phone, &checked); err != nil {
			return c, err
		}
		cell.Type = "PARKING"
		if uid != "" {
			cell.Occupant = &domain.Occupant{UserID: uid, FullName: name, VehicleName: vehicle, CheckedInAt: checked.Time}
			if domain.IsAdmin(c.Role) || uid == viewerID {
				cell.Occupant.LicensePlate = plate
				cell.Occupant.Email = email
				cell.Occupant.Phone = phone
			}
		}
		c.Cells = append(c.Cells, cell)
	}
	if err := rows.Err(); err != nil {
		return c, err
	}
	sort.Slice(c.Cells, func(i, j int) bool {
		if c.Cells[i].Row == c.Cells[j].Row {
			return c.Cells[i].Col < c.Cells[j].Col
		}
		return c.Cells[i].Row < c.Cells[j].Row
	})
	return c, nil
}

func validateLayout(in domain.LayoutInput) error {
	if in.Rows < 1 || in.Rows > 50 || in.Cols < 1 || in.Cols > 50 {
		return platform.E(400, "INVALID_GRID_SIZE", "Grid dimensions must be between 1 and 50.")
	}
	allowed := map[string]bool{"EMPTY": true, "PARKING": true, "ROAD": true, "ENTRY_GATE": true, "EXIT_GATE": true, "WALL": true, "PILLAR": true, "NO_PARKING": true}
	positions := map[string]bool{}
	names := map[string]bool{}
	for _, c := range in.Cells {
		if c.Row < 0 || c.Row >= in.Rows || c.Col < 0 || c.Col >= in.Cols {
			return platform.E(400, "CELL_OUT_OF_BOUNDS", "A layout cell is outside the grid.")
		}
		if !allowed[c.Type] {
			return platform.E(400, "INVALID_CELL_TYPE", "The layout contains an invalid cell type.")
		}
		key := fmt.Sprintf("%d:%d", c.Row, c.Col)
		if positions[key] {
			return platform.E(400, "DUPLICATE_CELL", "Only one element is allowed at each position.")
		}
		positions[key] = true
		if c.Type == "PARKING" {
			name := strings.ToUpper(strings.TrimSpace(c.Label))
			if name == "" {
				return platform.E(400, "SLOT_NAME_REQUIRED", "Every parking spot needs a name.")
			}
			if names[name] {
				return platform.E(400, "DUPLICATE_SLOT_NAME", "Parking spot names must be unique.")
			}
			names[name] = true
		}
	}
	return nil
}

func (s *Store) SaveLayout(ctx context.Context, communityID string, in domain.LayoutInput) (uint64, error) {
	if err := validateLayout(in); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var current uint64
	if err = tx.QueryRowContext(ctx, `SELECT layout_version FROM communities WHERE id=? FOR UPDATE`, communityID).Scan(&current); err != nil {
		return 0, err
	}
	if current != in.LayoutVersion {
		return 0, platform.E(409, "LAYOUT_VERSION_CONFLICT", "The layout changed elsewhere. Refresh and try again.")
	}
	type existingSlot struct {
		ID, Name, Status string
		Row, Col         int
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,slot_name,row_idx,col_idx,status FROM parking_slots WHERE community_id=? AND archived_at IS NULL FOR UPDATE`, communityID)
	if err != nil {
		return 0, err
	}
	existing := map[string]existingSlot{}
	for rows.Next() {
		var x existingSlot
		if err = rows.Scan(&x.ID, &x.Name, &x.Row, &x.Col, &x.Status); err != nil {
			rows.Close()
			return 0, err
		}
		existing[fmt.Sprintf("%d:%d", x.Row, x.Col)] = x
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	desired := map[string]domain.MapCell{}
	static := []domain.MapCell{}
	for _, cell := range in.Cells {
		key := fmt.Sprintf("%d:%d", cell.Row, cell.Col)
		if cell.Type == "PARKING" {
			desired[key] = cell
		} else if cell.Type != "EMPTY" {
			cell.Status = ""
			cell.SlotID = ""
			cell.Occupant = nil
			static = append(static, cell)
		}
	}
	for key, old := range existing {
		cell, ok := desired[key]
		if old.Status == "OCCUPIED" && (!ok || strings.TrimSpace(cell.Label) != old.Name) {
			return 0, platform.E(409, "OCCUPIED_SLOT_IMMUTABLE", "Occupied spots cannot be moved, renamed, or removed.")
		}
		if !ok {
			if _, err = tx.ExecContext(ctx, `UPDATE parking_slots SET archived_at=UTC_TIMESTAMP(6) WHERE id=? AND status='AVAILABLE'`, old.ID); err != nil {
				return 0, err
			}
			continue
		}
		if strings.TrimSpace(cell.Label) != old.Name {
			if _, err = tx.ExecContext(ctx, `UPDATE parking_slots SET slot_name=? WHERE id=?`, strings.TrimSpace(cell.Label), old.ID); err != nil {
				if duplicate(err) {
					return 0, platform.E(409, "DUPLICATE_SLOT_NAME", "Parking spot names must be unique.")
				}
				return 0, err
			}
		}
		delete(desired, key)
	}
	for _, cell := range desired {
		_, err = tx.ExecContext(ctx, `INSERT INTO parking_slots (id,community_id,slot_name,row_idx,col_idx) VALUES (?,?,?,?,?)`, platform.NewID(), communityID, strings.TrimSpace(cell.Label), cell.Row, cell.Col)
		if err != nil {
			if duplicate(err) {
				return 0, platform.E(409, "DUPLICATE_SLOT", "Parking spot name and position must be unique.")
			}
			return 0, err
		}
	}
	layout, _ := json.Marshal(map[string]any{"cells": static})
	next := current + 1
	if _, err = tx.ExecContext(ctx, `UPDATE communities SET grid_rows=?,grid_cols=?,layout_json=?,layout_version=? WHERE id=?`, in.Rows, in.Cols, layout, next, communityID); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

type OccupancyResult struct {
	OccupancyID string    `json:"occupancyId"`
	SlotID      string    `json:"slotId"`
	UserID      string    `json:"userId"`
	Status      string    `json:"status"`
	CheckedAt   time.Time `json:"checkedAt"`
}

func (s *Store) CheckIn(ctx context.Context, communityID, slotID, targetUserID, actorID, checkType string) (OccupancyResult, error) {
	var out OccupancyResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// Membership is locked before the slot everywhere that can revoke access.
	// This stable order prevents check-in/member-removal deadlocks.
	var memberStatus, vehicle, plate string
	if err = tx.QueryRowContext(ctx, `SELECT m.status,u.vehicle_name,u.license_plate FROM community_memberships m JOIN users u ON u.id=m.user_id WHERE m.community_id=? AND m.user_id=? FOR UPDATE`, communityID, targetUserID).Scan(&memberStatus, &vehicle, &plate); err != nil || memberStatus != "APPROVED" {
		return out, platform.E(403, "MEMBERSHIP_REQUIRED", "The selected user is not an approved member.")
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM occupancies WHERE community_id=? AND user_id=? AND checked_out_at IS NULL`, communityID, targetUserID).Scan(&active); err != nil {
		return out, err
	}
	if active > 0 {
		return out, platform.E(409, "USER_ALREADY_PARKED", "This user already occupies a spot in this community.")
	}
	var status, slotCommunity, slotName string
	if err = tx.QueryRowContext(ctx, `SELECT community_id,status,slot_name FROM parking_slots WHERE id=? AND archived_at IS NULL FOR UPDATE`, slotID).Scan(&slotCommunity, &status, &slotName); errors.Is(err, sql.ErrNoRows) {
		return out, platform.E(404, "SLOT_NOT_FOUND", "Parking spot not found.")
	}
	if err != nil {
		return out, err
	}
	if slotCommunity != communityID {
		return out, platform.E(404, "SLOT_NOT_FOUND", "Parking spot not found.")
	}
	if status != "AVAILABLE" {
		return out, platform.E(409, "SLOT_ALREADY_OCCUPIED", "This parking spot has already been occupied.")
	}
	out = OccupancyResult{OccupancyID: platform.NewID(), SlotID: slotID, UserID: targetUserID, Status: "OCCUPIED", CheckedAt: time.Now().UTC()}
	if _, err = tx.ExecContext(ctx, `INSERT INTO occupancies (id,community_id,parking_slot_id,user_id,slot_name_snapshot,vehicle_name_snapshot,license_plate_snapshot,check_in_type,checked_in_by_user_id) VALUES (?,?,?,?,?,?,?,?,?)`, out.OccupancyID, communityID, slotID, targetUserID, slotName, vehicle, plate, checkType, actorID); err != nil {
		if duplicate(err) {
			return out, platform.E(409, "USER_ALREADY_PARKED", "The user or spot already has an active occupancy.")
		}
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE parking_slots SET status='OCCUPIED',occupied_by_user_id=?,active_occupancy_id=? WHERE id=? AND status='AVAILABLE'`, targetUserID, out.OccupancyID, slotID); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) CheckOut(ctx context.Context, communityID, slotID, actorID string, force bool) (OccupancyResult, error) {
	var out OccupancyResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var slotCommunity, status, occupantID, occupancyID string
	err = tx.QueryRowContext(ctx, `SELECT community_id,status,COALESCE(occupied_by_user_id,''),COALESCE(active_occupancy_id,'') FROM parking_slots WHERE id=? AND archived_at IS NULL FOR UPDATE`, slotID).Scan(&slotCommunity, &status, &occupantID, &occupancyID)
	if errors.Is(err, sql.ErrNoRows) || slotCommunity != communityID {
		return out, platform.E(404, "SLOT_NOT_FOUND", "Parking spot not found.")
	}
	if err != nil {
		return out, err
	}
	if status != "OCCUPIED" {
		return out, platform.E(409, "SLOT_NOT_OCCUPIED", "This parking spot is already available.")
	}
	if !force && occupantID != actorID {
		return out, platform.E(403, "NOT_SLOT_OCCUPANT", "Only the occupant can check out this spot.")
	}
	checkoutType := "SELF"
	if force {
		checkoutType = "ADMIN_FORCE"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE occupancies SET checked_out_at=UTC_TIMESTAMP(6),checked_out_by_user_id=?,checkout_type=? WHERE id=? AND checked_out_at IS NULL`, actorID, checkoutType, occupancyID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE parking_slots SET status='AVAILABLE',occupied_by_user_id=NULL,active_occupancy_id=NULL WHERE id=?`, slotID); err != nil {
		return out, err
	}
	out = OccupancyResult{OccupancyID: occupancyID, SlotID: slotID, UserID: occupantID, Status: "AVAILABLE", CheckedAt: time.Now().UTC()}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
