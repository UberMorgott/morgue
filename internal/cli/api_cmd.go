package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const apiBase = "http://127.0.0.1:19876/api"

var errNotRunning = fmt.Errorf("GUI is not running. Launch morgue.exe first, or use 'morgue run' for headless mode")

// printJSON decodes JSON from r and pretty-prints it with indentation to stdout.
func printJSON(r io.Reader) error {
	var v any
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func apiGet(path string) error {
	resp, err := http.Get(apiBase + path)
	if err != nil {
		return errNotRunning
	}
	defer resp.Body.Close()
	return printJSON(resp.Body)
}

func apiPost(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := http.Post(apiBase+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return errNotRunning
	}
	defer resp.Body.Close()
	return printJSON(resp.Body)
}

func apiPut(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPut, apiBase+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errNotRunning
	}
	defer resp.Body.Close()
	return printJSON(resp.Body)
}

// APIStatus prints the current status of the running GUI.
func APIStatus() error {
	return apiGet("/status")
}

// APIRun sends a decompilation run request to the GUI.
func APIRun(path, output string) error {
	return apiPost("/run", map[string]string{"path": path, "output": output})
}

// APITools lists available tools from the running GUI.
func APITools() error {
	return apiGet("/tools")
}

// APIToolsInstall installs a tool by name via the GUI.
func APIToolsInstall(name string) error {
	return apiPost("/tools/install", map[string]string{"name": name})
}

// APIToolsDelete deletes a tool by name via the GUI.
func APIToolsDelete(name string) error {
	return apiPost("/tools/delete", map[string]string{"name": name})
}

// APISettings retrieves current settings from the GUI.
func APISettings() error {
	return apiGet("/settings")
}

// APISettingsSet updates a setting by key/value via the GUI.
func APISettingsSet(key, value string) error {
	return apiPut("/settings", map[string]string{"key": key, "value": value})
}
