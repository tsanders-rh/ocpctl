-- Migration: Add authentication and authorization
-- Creates users table, refresh_tokens table, and adds owner_id to clusters

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'USER',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(active);
CREATE INDEX idx_users_role ON users(role);

-- Refresh tokens table (for token revocation)
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMP
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

-- Add owner_id to clusters table
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);
CREATE INDEX idx_clusters_owner_id ON clusters(owner_id);

-- Create default admin user (password: changeme)
-- This should be changed immediately after first login
INSERT INTO users (id, email, username, password_hash, role, active)
VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'admin@localhost',
    'Admin User',
    '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYb0UqRh.Ju', -- bcrypt hash of 'changeme'
    'ADMIN',
    true
)
ON CONFLICT (email) DO NOTHING;

-- Assign existing clusters to admin user
UPDATE clusters
SET owner_id = 'a0000000-0000-0000-0000-000000000001'
WHERE owner_id IS NULL;
