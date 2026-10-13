# Architecture — first vertical slice

## Trust boundaries

- Administrator client → server API: not authenticated in this prototype.
- Agent → server API: bootstrap token only during enrollment; per-agent random secret for heartbeat.
- Server → PostgreSQL: private Compose network.

## Device identity

- Server generates a UUID and a 256-bit random secret.
- The secret is returned once at enrollment; only SHA-256(secret) is stored server-side.
- Agent stores the secret in a local state file. This is not suitable for a production privileged service; use Windows DPAPI/Credential Manager or another OS-backed secret store.
- Enrollment is not idempotent yet: deleting agent state and re-enrolling creates a new device record. A later version should add controlled re-enrollment and revocation.

## Device state

The database stores `last_seen_at`. API response derives `online` if `now - last_seen_at <= OFFLINE_AFTER`, otherwise `offline`. A device that has not enrolled does not yet exist in the database, so “not registered” is a UI state rather than a device status.

## Next hardening steps

1. TLS termination and secure deployment defaults.
2. Admin login, sessions, CSRF strategy if cookie-based, RBAC, and authorization middleware.
3. Agent secret revocation/rotation and enrollment-token rotation.
4. Audit events and security events.
5. Request rate limits, stricter validation, metrics and tracing.
6. WebSocket transport with authenticated session establishment, reconnect/backoff, protocol versioning and bounded message sizes.
7. Windows Service wrapper, OS-backed secret storage and signed installer/update packages.
8. Integration tests against a disposable PostgreSQL instance.
