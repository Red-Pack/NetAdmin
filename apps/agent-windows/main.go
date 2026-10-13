package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const agentVersion = "0.1.0"

type state struct {
	DeviceID    string `json:"device_id"`
	AgentSecret string `json:"agent_secret"`
}

type registerResponse struct {
	DeviceID    string `json:"device_id"`
	AgentSecret string `json:"agent_secret"`
}

type agentInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	AgentVersion string `json:"agent_version"`
}

func main() {
	serverURL := strings.TrimRight(os.Getenv("SERVER_URL"), "/")
	enrollmentToken := os.Getenv("ENROLLMENT_TOKEN")
	stateFile := os.Getenv("AGENT_STATE_FILE")
	if serverURL == "" || enrollmentToken == "" {
		fatal("SERVER_URL and ENROLLMENT_TOKEN environment variables are required")
	}
	if stateFile == "" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = "."
		}
		stateFile = filepath.Join(programData, "RemoteAdmin", "agent-state.json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	info, err := collectInfo()
	if err != nil {
		fatal("collect system info: " + err.Error())
	}
	st, err := loadState(stateFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fatal("load state: " + err.Error())
		}
		st, err = register(client, serverURL, enrollmentToken, info)
		if err != nil {
			fatal("register agent: " + err.Error())
		}
		if err := saveState(stateFile, st); err != nil {
			fatal("save state: " + err.Error())
		}
		fmt.Printf("Registered device %s\n", st.DeviceID)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	ctxCh := make(chan os.Signal, 1)
	signal.Notify(ctxCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(ctxCh)

	if err := sendHeartbeat(client, serverURL, st, info); err != nil {
		fmt.Fprintln(os.Stderr, "heartbeat failed:", err)
	}
	for {
		select {
		case <-ctxCh:
			fmt.Println("Stopping agent")
			return
		case <-ticker.C:
			info, err = collectInfo()
			if err != nil {
				fmt.Fprintln(os.Stderr, "collect system info failed:", err)
				continue
			}
			if err := sendHeartbeat(client, serverURL, st, info); err != nil {
				fmt.Fprintln(os.Stderr, "heartbeat failed:", err)
			}
		}
	}
}

func collectInfo() (agentInfo, error) {
	host, err := os.Hostname()
	if err != nil {
		return agentInfo{}, err
	}
	return agentInfo{
		Hostname: host, OS: runtime.GOOS,
		Architecture: runtime.GOARCH, AgentVersion: agentVersion,
	}, nil
}

func register(client *http.Client, baseURL, enrollment string, info agentInfo) (state, error) {
	body := map[string]string{
		"enrollment_token": enrollment,
		"hostname": info.Hostname, "os": info.OS,
		"architecture": info.Architecture, "agent_version": info.AgentVersion,
	}
	var result registerResponse
	if err := doJSON(client, http.MethodPost, baseURL+"/api/v1/agents/register", "", body, &result, http.StatusCreated); err != nil {
		return state{}, err
	}
	if result.DeviceID == "" || result.AgentSecret == "" {
		return state{}, errors.New("server returned incomplete credentials")
	}
	return state{DeviceID: result.DeviceID, AgentSecret: result.AgentSecret}, nil
}

func sendHeartbeat(client *http.Client, baseURL string, st state, info agentInfo) error {
	return doJSON(client, http.MethodPost, baseURL+"/api/v1/agents/heartbeat",
		st.AgentSecret, info, nil, http.StatusNoContent)
}

func doJSON(client *http.Client, method, url, secret string, body any, out any, expectedStatus int) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, url)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

func loadState(path string) (state, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return state{}, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return state{}, err
	}
	if st.DeviceID == "" || st.AgentSecret == "" {
		return state{}, errors.New("state file has missing credentials")
	}
	return st, nil
}

func saveState(path string, st state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

