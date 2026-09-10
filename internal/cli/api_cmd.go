package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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

// apiDo issues a request against the local GUI's HTTP API. The CLI commands are
// one-shot and have no cancellable context of their own, so the background
// context is what these carry.
func apiDo(method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, apiBase+path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return http.DefaultClient.Do(req)
}

func apiGet(path string) error {
	resp, err := apiDo(http.MethodGet, path, "", nil)
	if err != nil {
		return errNotRunning
	}
	defer func() { _ = resp.Body.Close() }()
	return printJSON(resp.Body)
}

func apiSend(method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	resp, err := apiDo(method, path, "application/json", bytes.NewReader(body))
	if err != nil {
		return errNotRunning
	}
	defer func() { _ = resp.Body.Close() }()
	return printJSON(resp.Body)
}

func apiPost(path string, payload any) error {
	return apiSend(http.MethodPost, path, payload)
}

func apiPut(path string, payload any) error {
	return apiSend(http.MethodPut, path, payload)
}

// APIStatus prints the current status of the running GUI.
func APIStatus() error {
	return apiGet("/status")
}

// APIRun sends a decompilation run request to the GUI (direct execution).
func APIRun(path, output string) error {
	return apiPost("/run", map[string]string{"path": path, "output": output})
}

// APIRunWait starts a decompilation run and polls /api/run/status until it completes.
func APIRunWait(path, output string) error {
	// Start the run with direct execution so we can poll status
	if err := apiPost("/run?direct=true", map[string]string{"path": path, "output": output}); err != nil {
		return err
	}

	// Poll status until pipeline finishes
	type status struct {
		Running        bool   `json:"running"`
		Phase          string `json:"phase"`
		StepName       string `json:"stepName"`
		StepIndex      int    `json:"stepIndex"`
		StepTotal      int    `json:"stepTotal"`
		FilesProcessed int    `json:"filesProcessed"`
		FilesTotal     int    `json:"filesTotal"`
	}

	lastPhase := ""
	for {
		time.Sleep(1 * time.Second)

		resp, err := apiDo(http.MethodGet, "/run/status", "", nil)
		if err != nil {
			return errNotRunning
		}
		var st status
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf("decode status: %w", err)
		}
		_ = resp.Body.Close()

		if !st.Running {
			fmt.Println("Pipeline complete.")
			return nil
		}

		// Print progress when phase or step changes
		desc := st.Phase
		if st.StepName != "" {
			desc = fmt.Sprintf("%s — %s (%d/%d)", st.Phase, st.StepName, st.StepIndex, st.StepTotal)
		}
		if st.FilesTotal > 0 {
			desc += fmt.Sprintf(" [%d/%d files]", st.FilesProcessed, st.FilesTotal)
		}
		if desc != lastPhase {
			fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05"), desc)
			lastPhase = desc
		}
	}
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
		resp, err := apiDo(http.MethodGet, "/tools?wait=30", "", nil)
		if err != nil {
			return errNotRunning
		}
		var result toolsResp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf("decode response: %w", err)
		}
		_ = resp.Body.Close()

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

// settingTypes maps config field names to their Go types for proper JSON encoding.
var settingTypes = map[string]string{
	// bool fields
	"SkipSystemLibs":        "bool",
	"KeepIntermediates":     "bool",
	"GenerateCallgraph":     "bool",
	"GenerateDotFiles":      "bool",
	"StopOnFirstError":      "bool",
	"PreferSystemTools":     "bool",
	"AutoUpdateCheck":       "bool",
	"AutoUpdateTools":       "bool",
	"AutoUpdateApp":         "bool",
	"GeneratePDB":           "bool",
	"DecompileProjectMode":  "bool",
	"LogToFile":             "bool",
	"LogTimestamps":         "bool",
	"AllowDynamicExecution": "bool",
	"SandboxWarning":        "bool",
	"UE5ExtractPAK":         "bool",
	"UE5SDKDump":            "bool",
	"UE5ExtractStrings":     "bool",
	"UE5GhidraDecompile":    "bool",
	"UE5NameResolution":     "bool",
	"UE5BuildIndexes":       "bool",
	"UE5ExportHookable":     "bool",
	"UnityExtractAssets":    "bool",
	// int fields
	"StepTimeoutMinutes":     "int",
	"MaxFileSizeMB":          "int",
	"ConcurrentTargets":      "int",
	"DownloadRetries":        "int",
	"DownloadTimeoutMinutes": "int",
}

// APISettingsSet updates a setting by key/value via the GUI.
// It converts the string value to the appropriate type (bool/int) for JSON encoding.
func APISettingsSet(key, value string) error {
	var typedValue any

	switch settingTypes[key] {
	case "bool":
		switch value {
		case "true", "1", "yes":
			typedValue = true
		case "false", "0", "no":
			typedValue = false
		default:
			return fmt.Errorf("invalid boolean value %q for %s (use true/false)", value, key)
		}
	case "int":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer value %q for %s", value, key)
		}
		typedValue = n
	default:
		typedValue = value
	}

	return apiPut("/settings", map[string]any{"key": key, "value": typedValue})
}
