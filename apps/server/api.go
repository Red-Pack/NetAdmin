package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"
)

type api struct {
	db     *pgxpool.Pool
	cfg    config
	logger *slog.Logger
}

func newAPI(db *pgxpool.Pool, cfg config, logger *slog.Logger) *api {
	return &api{db: db, cfg: cfg, logger: logger}
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/agents/register", a.registerAgent)
	mux.HandleFunc("POST /api/v1/agents/heartbeat", a.heartbeat)
	mux.HandleFunc("GET /api/v1/devices", a.listDevices)
	return requestLog(a.logger, mux)
}

type registerRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Hostname         string `json:"hostname"`
	OS               string `json:"os"`
	Architecture      string `json:"architecture"`
	AgentVersion      string `json:"agent_version"`
}

type heartbeatRequest struct {
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Architecture string `json:"architecture"`
	AgentVersion string `json:"agent_version"`
}

type deviceResponse struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	AgentVersion string    `json:"agent_version"`
	Status       string    `json:"status"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	LastIP       *string   `json:"last_ip,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *api) registerAgent(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.EnrollmentToken), []byte(a.cfg.EnrollmentToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_enrollment_token")
		return
	}
	if err := validateAgentInfo(req.Hostname, req.OS, req.Architecture, req.AgentVersion); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	deviceID := uuid.New()
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		a.logger.Error("generate agent secret", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	hash := sha256.Sum256([]byte(secret))
	now := time.Now().UTC()
	ip := remoteIP(r)

	_, err := a.db.Exec(r.Context(), `
		INSERT INTO devices (id, hostname, os, architecture, agent_version, agent_secret_hash, last_seen_at, last_ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::inet)`,
		deviceID, strings.TrimSpace(req.Hostname), strings.TrimSpace(req.OS),
		strings.TrimSpace(req.Architecture), strings.TrimSpace(req.AgentVersion), hash[:], now, ip)
	if err != nil {
		a.logger.Error("insert device", "error", err)
		writeError(w, http.StatusInternalServerError, "database_error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"device_id": deviceID.String(),
		"agent_secret": secret,
	})
}

func (a *api) heartbeat(w http.ResponseWriter, r *http.Request) {
	secret, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing_agent_credentials")
		return
	}
	var req heartbeatRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := validateAgentInfo(req.Hostname, req.OS, req.Architecture, req.AgentVersion); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hash := sha256.Sum256([]byte(secret))
	ip := remoteIP(r)
	tag, err := a.db.Exec(r.Context(), `
		UPDATE devices
		SET hostname=$1, os=$2, architecture=$3, agent_version=$4,
		    last_seen_at=now(), last_ip=NULLIF($5, '')::inet, updated_at=now()
		WHERE agent_secret_hash=$6`,
		strings.TrimSpace(req.Hostname), strings.TrimSpace(req.OS),
		strings.TrimSpace(req.Architecture), strings.TrimSpace(req.AgentVersion), ip, hash[:])
	if err != nil {
		a.logger.Error("update heartbeat", "error", err)
		writeError(w, http.StatusInternalServerError, "database_error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusUnauthorized, "invalid_agent_credentials")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listDevices(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT id, hostname, os, architecture, agent_version, last_seen_at,
		       host(last_ip), created_at
		FROM devices ORDER BY hostname ASC`)
	if err != nil {
		a.logger.Error("query devices", "error", err)
		writeError(w, http.StatusInternalServerError, "database_error")
		return
	}
	defer rows.Close()

	devices := make([]deviceResponse, 0)
	now := time.Now().UTC()
	for rows.Next() {
		var d deviceResponse
		var lastIP *string
		if err := rows.Scan(&d.ID, &d.Hostname, &d.OS, &d.Architecture, &d.AgentVersion, &d.LastSeenAt, &lastIP, &d.CreatedAt); err != nil {
			a.logger.Error("scan device", "error", err)
			writeError(w, http.StatusInternalServerError, "database_error")
			return
		}
		d.LastIP = lastIP
		if now.Sub(d.LastSeenAt) <= a.cfg.OfflineAfter {
			d.Status = "online"
		} else {
			d.Status = "offline"
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		a.logger.Error("iterate devices", "error", err)
		writeError(w, http.StatusInternalServerError, "database_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": devices, "count": len(devices)})
}

func validateAgentInfo(hostname, osName, arch, version string) error {
	if strings.TrimSpace(hostname) == "" || len(hostname) > 255 {
		return errors.New("invalid_hostname")
	}
	if strings.TrimSpace(osName) == "" || len(osName) > 64 {
		return errors.New("invalid_os")
	}
	if strings.TrimSpace(arch) == "" || len(arch) > 64 {
		return errors.New("invalid_architecture")
	}
	if strings.TrimSpace(version) == "" || len(version) > 64 {
		return errors.New("invalid_agent_version")
	}
	return nil
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || strings.TrimSpace(strings.TrimPrefix(header, prefix)) == "" {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix)), true
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(), "remote_addr", r.RemoteAddr)
	})
}

