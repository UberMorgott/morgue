package recipe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// StringsAnalysis is the structured output of string analysis.
type StringsAnalysis struct {
	TotalRaw      int      `json:"total_raw"`
	TotalFiltered int      `json:"total_filtered"`
	URLs          []string `json:"urls"`
	Paths         []string `json:"paths"`
	Errors        []string `json:"errors"`
	Config        []string `json:"config"`
	APIKeys       []string `json:"api_keys"`
	Interesting   []string `json:"interesting"`
}

var (
	reURL         = regexp.MustCompile(`(?i)^https?://|://`)
	rePath        = regexp.MustCompile(`(?i)^[A-Z]:\\|[/\\][a-zA-Z0-9_.+-]+[/\\]`)
	reError       = regexp.MustCompile(`(?i)(error|exception|fail|invalid|cannot|unable)`)
	reConfigKV    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*\s*=\s*.+`)
	reAPIKey      = regexp.MustCompile(`(?i)(key|token|secret|password|api)`)
	reJustNumbers = regexp.MustCompile(`^[\d.,]+$`)
	reHTMLTag     = regexp.MustCompile(`^</?[a-zA-Z][^>]*>$`)
	reBase64Like  = regexp.MustCompile(`^[A-Za-z0-9+/=]{40,}$`)
	reMIME        = regexp.MustCompile(`^(?i)[a-z]+/[a-z0-9.+-]+$`)
	reFileExt     = regexp.MustCompile(`^\.[a-zA-Z0-9]{1,10}$`)
)

// analyzeStrings reads a raw strings.txt file, categorizes the entries,
// and writes a structured JSON output. Errors are logged but do not
// propagate — callers should not fail the pipeline step on analysis failure.
func analyzeStrings(stringsFile, outputFile string) {
	if err := doAnalyzeStrings(stringsFile, outputFile); err != nil {
		log.Printf("strings analysis: %v", err)
	}
}

// doAnalyzeStrings performs the actual analysis work.
func doAnalyzeStrings(stringsFile, outputFile string) error {
	f, err := os.Open(stringsFile)
	if err != nil {
		return fmt.Errorf("open %s: %w", stringsFile, err)
	}
	defer f.Close()

	urlSet := map[string]bool{}
	pathSet := map[string]bool{}
	errorSet := map[string]bool{}
	configSet := map[string]bool{}
	apiKeySet := map[string]bool{}
	interestingSet := map[string]bool{}

	totalRaw := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		totalRaw++
		line := strings.TrimSpace(scanner.Text())

		if shouldSkipString(line) {
			continue
		}

		categorized := false

		// URLs
		if reURL.MatchString(line) {
			urlSet[line] = true
			categorized = true
		}

		// Paths
		if rePath.MatchString(line) {
			pathSet[line] = true
			categorized = true
		}

		// Errors
		if reError.MatchString(line) {
			errorSet[line] = true
			categorized = true
		}

		// Config (KEY=VALUE)
		if reConfigKV.MatchString(line) {
			configSet[line] = true
			categorized = true
		}

		// API keys: must match key-like keyword AND have a value-like part (= or :)
		if reAPIKey.MatchString(line) && (strings.Contains(line, "=") || strings.Contains(line, ":")) {
			apiKeySet[line] = true
			categorized = true
		}

		// Interesting: longer than 20 chars, not already categorized, not garbage
		if !categorized && len(line) > 20 {
			interestingSet[line] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", stringsFile, err)
	}

	analysis := StringsAnalysis{
		TotalRaw:      totalRaw,
		TotalFiltered: len(urlSet) + len(pathSet) + len(errorSet) + len(configSet) + len(apiKeySet) + len(interestingSet),
		URLs:          sortedKeys(urlSet),
		Paths:         sortedKeys(pathSet),
		Errors:        sortedKeys(errorSet),
		Config:        sortedKeys(configSet),
		APIKeys:       sortedKeys(apiKeySet),
		Interesting:   sortedKeys(interestingSet),
	}

	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputFile, err)
	}
	return nil
}

// shouldSkipString returns true for noise strings that should be filtered out.
func shouldSkipString(s string) bool {
	// Too short
	if len(s) < 4 {
		return true
	}
	// Just numbers
	if reJustNumbers.MatchString(s) {
		return true
	}
	// HTML/XML tag only
	if reHTMLTag.MatchString(s) {
		return true
	}
	// Base64-like (long alphanumeric with no spaces)
	if !strings.Contains(s, " ") && reBase64Like.MatchString(s) {
		return true
	}
	// File extension only (.dll, .exe, etc.)
	if reFileExt.MatchString(s) {
		return true
	}
	// MIME type only
	if reMIME.MatchString(s) {
		return true
	}
	// All non-printable or control chars
	if isGarbage(s) {
		return true
	}
	return false
}

// isGarbage returns true if the string is mostly non-printable characters.
func isGarbage(s string) bool {
	printable := 0
	total := 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) {
			printable++
		}
	}
	if total == 0 {
		return true
	}
	return float64(printable)/float64(total) < 0.7
}

// sortedKeys returns sorted, deduplicated keys from a set.
func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
