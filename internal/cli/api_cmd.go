package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
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

// APIToolsWait polls the tools endpoint with long-poll until no operations are busy.
func APIToolsWait() error {
	type toolEntry struct {
		Name       string `json:"Name"`
		Installing bool   `json:"installing"`
		Progress   int    `json:"progress"`
	}
	type toolsResp struct {
		Tools   []toolEntry `json:"tools"`
		Busy    bool        `json:"busy"`
		Changed bool        `json:"changed"`
	}

	for {
		resp, err := http.Get(apiBase + "/tools?wait=30")
		if err != nil {
			return errNotRunning
		}
		var result toolsResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return fmt.Errorf("decode response: %w", err)
		}
		resp.Body.Close()

		// Print active operations
		for _, t := range result.Tools {
			if t.Installing {
				fmt.Printf("  %s: installing (%d%%)\n", t.Name, t.Progress)
			}
		}

		if !result.Busy {
			fmt.Println("All tool operations complete.")
			return nil
		}

		// If not changed (timeout), print status and keep polling
		if !result.Changed {
			fmt.Printf("[%s] Waiting for operations to finish...\n", time.Now().Format("15:04:05"))
		}
	}
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
