CREATE TABLE IF NOT EXISTS devices (
    id UUID PRIMARY KEY,
    hostname TEXT NOT NULL,
    os TEXT NOT NULL,
    architecture TEXT NOT NULL,
    agent_version TEXT NOT NULL,
    agent_secret_hash BYTEA NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    last_ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_devices_last_seen_at ON devices (last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices (hostname);
