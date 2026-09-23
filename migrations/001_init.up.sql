BEGIN;

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(32) PRIMARY KEY,
    full_name VARCHAR(120) NOT NULL,
    email VARCHAR(254) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    phone VARCHAR(40) NOT NULL,
    vehicle_name VARCHAR(120) NOT NULL,
    license_plate VARCHAR(40) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_email ON users (LOWER(email));

CREATE TABLE IF NOT EXISTS communities (
    id VARCHAR(32) PRIMARY KEY,
    name VARCHAR(140) NOT NULL,
    code VARCHAR(16) NOT NULL,
    description VARCHAR(500),
    address VARCHAR(255),
    owner_user_id VARCHAR(32) NOT NULL,
    grid_rows SMALLINT NOT NULL,
    grid_cols SMALLINT NOT NULL,
    layout_json JSONB NOT NULL DEFAULT '{"cells":[]}'::jsonb,
    layout_version BIGINT NOT NULL DEFAULT 1,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_communities_owner FOREIGN KEY (owner_user_id) REFERENCES users(id),
    CONSTRAINT chk_grid_rows CHECK (grid_rows BETWEEN 1 AND 50),
    CONSTRAINT chk_grid_cols CHECK (grid_cols BETWEEN 1 AND 50),
    CONSTRAINT chk_layout_version CHECK (layout_version >= 1),
    CONSTRAINT chk_community_status CHECK (status IN ('ACTIVE', 'ARCHIVED'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_communities_code ON communities (UPPER(code));
CREATE INDEX IF NOT EXISTS idx_communities_name ON communities (LOWER(name));

CREATE TABLE IF NOT EXISTS community_memberships (
    id VARCHAR(32) PRIMARY KEY,
    community_id VARCHAR(32) NOT NULL,
    user_id VARCHAR(32) NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'MEMBER',
    status VARCHAR(16) NOT NULL,
    requested_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    approved_at TIMESTAMPTZ(6),
    approved_by_user_id VARCHAR(32),
    status_reason VARCHAR(500),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_membership UNIQUE (community_id, user_id),
    CONSTRAINT fk_membership_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_membership_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_membership_approver FOREIGN KEY (approved_by_user_id) REFERENCES users(id),
    CONSTRAINT chk_membership_role CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),
    CONSTRAINT chk_membership_status CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'BANNED', 'LEFT', 'REMOVED'))
);

CREATE INDEX IF NOT EXISTS idx_membership_user_status ON community_memberships (user_id, status);
CREATE INDEX IF NOT EXISTS idx_membership_community_status ON community_memberships (community_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_approved_owner_per_community
    ON community_memberships (community_id)
    WHERE role = 'OWNER' AND status = 'APPROVED';

CREATE TABLE IF NOT EXISTS parking_slots (
    id VARCHAR(32) PRIMARY KEY,
    community_id VARCHAR(32) NOT NULL,
    slot_name VARCHAR(80) NOT NULL,
    row_idx SMALLINT NOT NULL,
    col_idx SMALLINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'AVAILABLE',
    occupied_by_user_id VARCHAR(32),
    active_occupancy_id VARCHAR(32),
    archived_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_slot_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_slot_occupant FOREIGN KEY (occupied_by_user_id) REFERENCES users(id),
    CONSTRAINT chk_slot_row CHECK (row_idx >= 0),
    CONSTRAINT chk_slot_col CHECK (col_idx >= 0),
    CONSTRAINT chk_slot_status CHECK (status IN ('AVAILABLE', 'OCCUPIED')),
    CONSTRAINT chk_slot_occupancy CHECK (
        (status = 'AVAILABLE' AND occupied_by_user_id IS NULL AND active_occupancy_id IS NULL)
        OR (status = 'OCCUPIED' AND occupied_by_user_id IS NOT NULL AND active_occupancy_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_active_slot_name
    ON parking_slots (community_id, UPPER(slot_name))
    WHERE archived_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_slot_position
    ON parking_slots (community_id, row_idx, col_idx)
    WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_slot_occupant ON parking_slots (occupied_by_user_id);

CREATE TABLE IF NOT EXISTS occupancies (
    id VARCHAR(32) PRIMARY KEY,
    community_id VARCHAR(32) NOT NULL,
    parking_slot_id VARCHAR(32) NOT NULL,
    user_id VARCHAR(32) NOT NULL,
    slot_name_snapshot VARCHAR(80) NOT NULL,
    vehicle_name_snapshot VARCHAR(120) NOT NULL,
    license_plate_snapshot VARCHAR(40) NOT NULL,
    check_in_type VARCHAR(24) NOT NULL,
    checked_in_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    checked_in_by_user_id VARCHAR(32) NOT NULL,
    checked_out_at TIMESTAMPTZ(6),
    checked_out_by_user_id VARCHAR(32),
    checkout_type VARCHAR(24),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_occupancy_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_occupancy_slot FOREIGN KEY (parking_slot_id) REFERENCES parking_slots(id),
    CONSTRAINT fk_occupancy_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_occupancy_checkin_actor FOREIGN KEY (checked_in_by_user_id) REFERENCES users(id),
    CONSTRAINT fk_occupancy_checkout_actor FOREIGN KEY (checked_out_by_user_id) REFERENCES users(id),
    CONSTRAINT chk_check_in_type CHECK (check_in_type IN ('SELF', 'ADMIN_FORCE')),
    CONSTRAINT chk_checkout_type CHECK (checkout_type IS NULL OR checkout_type IN ('SELF', 'ADMIN_FORCE', 'MEMBER_REMOVAL')),
    CONSTRAINT chk_checkout_fields CHECK (
        (checked_out_at IS NULL AND checked_out_by_user_id IS NULL AND checkout_type IS NULL)
        OR (checked_out_at IS NOT NULL AND checked_out_by_user_id IS NOT NULL AND checkout_type IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_active_user_per_community
    ON occupancies (community_id, user_id)
    WHERE checked_out_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_slot
    ON occupancies (parking_slot_id)
    WHERE checked_out_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_occupancy_community_time ON occupancies (community_id, checked_in_at DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_slot_active_occupancy'
          AND conrelid = 'parking_slots'::regclass
    ) THEN
        ALTER TABLE parking_slots
            ADD CONSTRAINT fk_slot_active_occupancy
            FOREIGN KEY (active_occupancy_id) REFERENCES occupancies(id);
    END IF;
END;
$$;

CREATE TABLE IF NOT EXISTS audit_logs (
    id VARCHAR(32) PRIMARY KEY,
    community_id VARCHAR(32),
    actor_user_id VARCHAR(32) NOT NULL,
    action_type VARCHAR(80) NOT NULL,
    target_user_id VARCHAR(32),
    target_slot_id VARCHAR(32),
    target_occupancy_id VARCHAR(32),
    metadata_json JSONB,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_audit_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_audit_community_time ON audit_logs (community_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor_time ON audit_logs (actor_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ(6) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ(6),
    CONSTRAINT uq_session_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_session_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_session_expiry ON sessions (expires_at);

DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_communities_updated_at ON communities;
CREATE TRIGGER trg_communities_updated_at BEFORE UPDATE ON communities
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_memberships_updated_at ON community_memberships;
CREATE TRIGGER trg_memberships_updated_at BEFORE UPDATE ON community_memberships
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_parking_slots_updated_at ON parking_slots;
CREATE TRIGGER trg_parking_slots_updated_at BEFORE UPDATE ON parking_slots
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

DROP TRIGGER IF EXISTS trg_occupancies_updated_at ON occupancies;
CREATE TRIGGER trg_occupancies_updated_at BEFORE UPDATE ON occupancies
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
