package gamedata

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// InputKey is one bound key within a chord.
type InputKey struct {
	Name             string
	InputSource      int
	DeadzoneOverride float64
}

var inputSourceLabels = map[int]string{
	0: "Key",
	1: "AxisTriggerPositive",
	2: "AxisTriggerNegative",
	3: "Axis",
}

var actionSectionLabels = map[int]string{
	0: "Tactical",
	1: "Geoscape",
	2: "Common",
}

var overridingBehaviorLabels = map[int]string{
	0: "Hidden",
	1: "Forbidden",
	2: "Allowed",
}

// organizeInput renders all InputMapDefs to input/inputmap.md (a human table)
// and input/inputmap.json (raw fields), returning the total action count.
func organizeInput(opts Options, inputMaps []map[string]any) int {
	if len(inputMaps) == 0 {
		return 0
	}
	md, total := renderInputMap(inputMaps)
	_ = writeJSON(filepath.Join(opts.OutDir, "input", "inputmap.json"), inputMaps)
	_ = writeFile(filepath.Join(opts.OutDir, "input", "inputmap.md"), md)
	return total
}

// renderInputMap builds the markdown table and counts actions. Exposed-ish for
// testing via package-internal callers.
func renderInputMap(inputMaps []map[string]any) (string, int) {
	var md strings.Builder
	md.WriteString("# Input Map\n\n")
	md.WriteString("| Action | Section | Chords (key : source[ : deadzone]) |\n")
	md.WriteString("|--------|---------|-------------------------------------|\n")
	total := 0
	for _, im := range inputMaps {
		for _, a := range asList(firstField(im, "Actions", "Action")) {
			am := asMap(a)
			if am == nil {
				continue
			}
			total++
			name := asString(firstField(am, "Name", "ActionName", "Id"))
			section := label(actionSectionLabels, asInt(firstField(am, "ActionSection", "Section")))
			md.WriteString(fmt.Sprintf("| %s | %s | %s |\n", mdEsc(name), section, mdEsc(renderChords(am))))
		}
	}
	return md.String(), total
}

// renderChords renders an action's Chords[] -> Keys[] as
// "[Behavior] k1 : src[ : dz] + k2 : src ; [Behavior] chord2...". The
// OverridingBehavior is a per-chord property (InputChord), matching the real
// Phoenix Point InputMapDef shape.
func renderChords(action map[string]any) string {
	var chordStrs []string
	for _, c := range asList(firstField(action, "Chords", "Chord")) {
		cm := asMap(c)
		if cm == nil {
			continue
		}
		var keyStrs []string
		for _, k := range asList(firstField(cm, "Keys", "Key")) {
			km := asMap(k)
			if km == nil {
				continue
			}
			ik := InputKey{
				Name:             asString(firstField(km, "Name", "Key", "InputKey")),
				InputSource:      asInt(firstField(km, "InputSource", "Source")),
				DeadzoneOverride: asFloat(firstField(km, "DeadzoneOverride", "Deadzone")),
			}
			seg := ik.Name + " : " + label(inputSourceLabels, ik.InputSource)
			if ik.DeadzoneOverride != 0 {
				seg += " : dz=" + strconv.FormatFloat(ik.DeadzoneOverride, 'g', -1, 64)
			}
			keyStrs = append(keyStrs, seg)
		}
		chord := strings.Join(keyStrs, " + ")
		if bv := firstField(cm, "OverridingBehavior", "Behavior"); bv != nil {
			chord = "[" + label(overridingBehaviorLabels, asInt(bv)) + "] " + chord
		}
		chordStrs = append(chordStrs, chord)
	}
	return strings.Join(chordStrs, " ; ")
}

// label returns the named label for v, or its number when unknown.
func label(m map[int]string, v int) string {
	if s, ok := m[v]; ok {
		return s
	}
	return strconv.Itoa(v)
}
