CREATE TABLE IF NOT EXISTS users (
    id CHAR(32) PRIMARY KEY,
    full_name VARCHAR(120) NOT NULL,
    email VARCHAR(254) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    phone VARCHAR(40) NOT NULL,
    vehicle_name VARCHAR(120) NOT NULL,
    license_plate VARCHAR(40) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_users_email (email)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS communities (
    id CHAR(32) PRIMARY KEY,
    name VARCHAR(140) NOT NULL,
    code VARCHAR(16) NOT NULL,
    description VARCHAR(500) NULL,
    address VARCHAR(255) NULL,
    owner_user_id CHAR(32) NOT NULL,
    grid_rows SMALLINT UNSIGNED NOT NULL,
    grid_cols SMALLINT UNSIGNED NOT NULL,
    layout_json JSON NOT NULL,
    layout_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    status ENUM('ACTIVE','ARCHIVED') NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_communities_code (code),
    KEY idx_communities_name (name),
    CONSTRAINT fk_communities_owner FOREIGN KEY (owner_user_id) REFERENCES users(id),
    CONSTRAINT chk_grid_rows CHECK (grid_rows BETWEEN 1 AND 50),
    CONSTRAINT chk_grid_cols CHECK (grid_cols BETWEEN 1 AND 50)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS community_memberships (
    id CHAR(32) PRIMARY KEY,
    community_id CHAR(32) NOT NULL,
    user_id CHAR(32) NOT NULL,
    role ENUM('OWNER','ADMIN','MEMBER') NOT NULL DEFAULT 'MEMBER',
    status ENUM('PENDING','APPROVED','REJECTED','BANNED','LEFT','REMOVED') NOT NULL,
    requested_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    approved_at TIMESTAMP(6) NULL,
    approved_by_user_id CHAR(32) NULL,
    status_reason VARCHAR(500) NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_membership (community_id, user_id),
    KEY idx_membership_user_status (user_id, status),
    KEY idx_membership_community_status (community_id, status),
    CONSTRAINT fk_membership_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_membership_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_membership_approver FOREIGN KEY (approved_by_user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS parking_slots (
    id CHAR(32) PRIMARY KEY,
    community_id CHAR(32) NOT NULL,
    slot_name VARCHAR(80) NOT NULL,
    row_idx SMALLINT UNSIGNED NOT NULL,
    col_idx SMALLINT UNSIGNED NOT NULL,
    status ENUM('AVAILABLE','OCCUPIED') NOT NULL DEFAULT 'AVAILABLE',
    occupied_by_user_id CHAR(32) NULL,
    active_occupancy_id CHAR(32) NULL,
    archived_at TIMESTAMP(6) NULL,
    active_name_guard VARCHAR(113) GENERATED ALWAYS AS (
      CASE WHEN archived_at IS NULL THEN CONCAT(community_id, ':', UPPER(slot_name)) ELSE NULL END
    ) STORED,
    active_position_guard VARCHAR(45) GENERATED ALWAYS AS (
      CASE WHEN archived_at IS NULL THEN CONCAT(community_id, ':', row_idx, ':', col_idx) ELSE NULL END
    ) STORED,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_active_slot_name (active_name_guard),
    UNIQUE KEY uq_active_slot_position (active_position_guard),
    KEY idx_slot_occupant (occupied_by_user_id),
    CONSTRAINT fk_slot_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_slot_occupant FOREIGN KEY (occupied_by_user_id) REFERENCES users(id),
    CONSTRAINT chk_slot_occupancy CHECK (
      (status = 'AVAILABLE' AND occupied_by_user_id IS NULL AND active_occupancy_id IS NULL)
      OR (status = 'OCCUPIED' AND occupied_by_user_id IS NOT NULL AND active_occupancy_id IS NOT NULL)
    )
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS occupancies (
    id CHAR(32) PRIMARY KEY,
    community_id CHAR(32) NOT NULL,
    parking_slot_id CHAR(32) NOT NULL,
    user_id CHAR(32) NOT NULL,
    slot_name_snapshot VARCHAR(80) NOT NULL,
    vehicle_name_snapshot VARCHAR(120) NOT NULL,
    license_plate_snapshot VARCHAR(40) NOT NULL,
    check_in_type ENUM('SELF','ADMIN_FORCE') NOT NULL,
    checked_in_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    checked_in_by_user_id CHAR(32) NOT NULL,
    checked_out_at TIMESTAMP(6) NULL,
    checked_out_by_user_id CHAR(32) NULL,
    checkout_type ENUM('SELF','ADMIN_FORCE','MEMBER_REMOVAL') NULL,
    active_user_guard VARCHAR(65) GENERATED ALWAYS AS (
      CASE WHEN checked_out_at IS NULL THEN CONCAT(community_id, ':', user_id) ELSE NULL END
    ) STORED,
    active_slot_guard CHAR(32) GENERATED ALWAYS AS (
      CASE WHEN checked_out_at IS NULL THEN parking_slot_id ELSE NULL END
    ) STORED,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_active_user_per_community (active_user_guard),
    UNIQUE KEY uq_active_slot (active_slot_guard),
    KEY idx_occupancy_community_time (community_id, checked_in_at),
    CONSTRAINT fk_occupancy_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_occupancy_slot FOREIGN KEY (parking_slot_id) REFERENCES parking_slots(id),
    CONSTRAINT fk_occupancy_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_occupancy_checkin_actor FOREIGN KEY (checked_in_by_user_id) REFERENCES users(id),
    CONSTRAINT fk_occupancy_checkout_actor FOREIGN KEY (checked_out_by_user_id) REFERENCES users(id)
) ENGINE=InnoDB;

ALTER TABLE parking_slots
    ADD CONSTRAINT fk_slot_active_occupancy FOREIGN KEY (active_occupancy_id) REFERENCES occupancies(id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id CHAR(32) PRIMARY KEY,
    community_id CHAR(32) NULL,
    actor_user_id CHAR(32) NOT NULL,
    action_type VARCHAR(80) NOT NULL,
    target_user_id CHAR(32) NULL,
    target_slot_id CHAR(32) NULL,
    target_occupancy_id CHAR(32) NULL,
    metadata_json JSON NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_audit_community_time (community_id, created_at),
    KEY idx_audit_actor_time (actor_user_id, created_at),
    CONSTRAINT fk_audit_community FOREIGN KEY (community_id) REFERENCES communities(id),
    CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS sessions (
    id CHAR(32) PRIMARY KEY,
    user_id CHAR(32) NOT NULL,
    token_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMP(6) NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_used_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    revoked_at TIMESTAMP(6) NULL,
    UNIQUE KEY uq_session_token_hash (token_hash),
    KEY idx_session_user (user_id),
    KEY idx_session_expiry (expires_at),
    CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB;
