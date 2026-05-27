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
Install all:      morgue api tools install
Install one:      morgue api tools install <name>
Delete tool:      morgue api tools delete <name>
Get settings:     morgue api settings
Change setting:   morgue api settings set <Key> <Value>

## Workflow Example

1. morgue api status              — verify GUI is running
2. morgue api tools               — check which tools are installed
3. morgue api tools install       — install missing tools
4. morgue api run C:\game\game.exe — start decompilation

## Notes
- The user sees all changes in the GUI window in real-time
- Decompilation runs async — use 'morgue api status' to check progress
- Default output: ./decompiled (relative to morgue.exe location)
`

func (s *Server) handleInstructions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(AIInstructions))
}
