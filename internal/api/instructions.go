package api

import "net/http"

// AIInstructions contains Markdown instructions for AI agents
// that control Morgue via the CLI/API.
const AIInstructions = `# Morgue — AI Control Instructions

You are controlling Morgue, a binary decompilation tool.
The GUI is running and the user can see your actions in real-time.

## Available Commands (run in terminal)

Check status:     morgue api status
Decompile:        morgue api run <file_path>
Decompile (out):  morgue api run <file_path> -o <output_dir>
List tools:       morgue api tools
List tools (wait): morgue api tools --wait
Install all:      morgue api tools install
Install one:      morgue api tools install <name>
Delete tool:      morgue api tools delete <name>
Get settings:     morgue api settings
Change setting:   morgue api settings set <Key> <Value>

## Tool Status API

GET /api/tools returns enriched tool status:

  {
    "tools": [
      {"Name": "diec", "Installed": true, "Version": "3.09"},
      {"Name": "nofuserex", "Installed": false, "installing": true,
       "progress": 45, "lastActivity": "2026-05-27T09:30:00Z"}
    ],
    "busy": true
  }

Fields:
- installing: true if a download/install is in progress for this tool
- progress: download progress percentage (0-100)
- lastActivity: timestamp of last progress update
- busy: true if any tool operation is in progress

### Long-poll with ?wait=N

GET /api/tools?wait=30 blocks up to 30 seconds until tool state changes.
Response includes "changed": true if state changed, false on timeout.
Max wait: 120 seconds. Use for efficient polling during installs.

### Hang detection

If a tool shows installing=true but lastActivity is older than 2 minutes,
the operation is likely hung. Consider retrying.

## Workflow Example

1. morgue api status              — verify GUI is running
2. morgue api tools               — check which tools are installed
3. morgue api tools install       — install missing tools
4. morgue api tools --wait        — wait until all installs finish
5. morgue api run C:\game\game.exe — start decompilation

## Notes
- The user sees all changes in the GUI window in real-time
- Decompilation runs async — use 'morgue api status' to check progress
- Default output: ./decompiled (relative to morgue.exe location)
- Use --wait flag or ?wait=N to efficiently monitor tool installations
`

func (s *Server) handleInstructions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(AIInstructions))
}
