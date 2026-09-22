package domain

import "time"

type User struct {
	ID           string    `json:"id"`
	FullName     string    `json:"fullName"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	VehicleName  string    `json:"vehicleName"`
	LicensePlate string    `json:"licensePlate,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Membership struct {
	ID          string     `json:"id"`
	CommunityID string     `json:"communityId"`
	UserID      string     `json:"userId"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	RequestedAt time.Time  `json:"requestedAt"`
	ApprovedAt  *time.Time `json:"approvedAt,omitempty"`
	User        User       `json:"user"`
}

type CommunityCard struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	Description    string `json:"description,omitempty"`
	Role           string `json:"role"`
	AvailableSpots int    `json:"availableSpots"`
	TotalSpots     int    `json:"totalSpots"`
}

type Community struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Code          string    `json:"code"`
	Description   string    `json:"description,omitempty"`
	Address       string    `json:"address,omitempty"`
	OwnerUserID   string    `json:"ownerUserId"`
	GridRows      int       `json:"gridRows"`
	GridCols      int       `json:"gridCols"`
	LayoutJSON    []byte    `json:"-"`
	LayoutVersion uint64    `json:"layoutVersion"`
	Role          string    `json:"role,omitempty"`
	Cells         []MapCell `json:"cells,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type MapCell struct {
	Row      int       `json:"row"`
	Col      int       `json:"col"`
	Type     string    `json:"type"`
	Label    string    `json:"label,omitempty"`
	SlotID   string    `json:"slotId,omitempty"`
	Status   string    `json:"status,omitempty"`
	Occupant *Occupant `json:"occupant,omitempty"`
}

type Occupant struct {
	UserID       string    `json:"userId"`
	FullName     string    `json:"fullName"`
	VehicleName  string    `json:"vehicleName"`
	LicensePlate string    `json:"licensePlate,omitempty"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	CheckedInAt  time.Time `json:"checkedInAt"`
}

type LayoutInput struct {
	Rows          int       `json:"rows"`
	Cols          int       `json:"cols"`
	LayoutVersion uint64    `json:"layoutVersion"`
	Cells         []MapCell `json:"cells"`
}

type AuditEvent struct {
	ID                string
	CommunityID       *string
	ActorUserID       string
	ActionType        string
	TargetUserID      *string
	TargetSlotID      *string
	TargetOccupancyID *string
	Metadata          map[string]any
}

type RealtimeEvent struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	CommunityID string         `json:"communityId"`
	SlotID      string         `json:"slotId,omitempty"`
	Version     uint64         `json:"version,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

const (
	RoleOwner  = "OWNER"
	RoleAdmin  = "ADMIN"
	RoleMember = "MEMBER"

	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusRejected = "REJECTED"
	StatusBanned   = "BANNED"
	StatusLeft     = "LEFT"
	StatusRemoved  = "REMOVED"
)

func IsAdmin(role string) bool { return role == RoleOwner || role == RoleAdmin }
