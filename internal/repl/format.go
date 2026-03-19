package repl

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// formatDuration formats a duration as a compact human-readable string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// toolInputSummary produces a compact one-line description of what a tool call does
// based on the tool name and input parameters. Used for the single-line tool display.
func toolInputSummary(name string, input map[string]any) string {
	switch name {
	case "checkfor":
		search, _ := input["search"].(string)
		dirs := countArray(input["dirs"])
		return fmt.Sprintf("%q in %d dirs", truncate(search, 40), dirs)

	case "repfor":
		search, _ := input["search"].(string)
		replace, _ := input["replace"].(string)
		dirs := countArray(input["dirs"])
		dryRun, _ := input["dry_run"].(bool)
		suffix := ""
		if dryRun {
			suffix = " (dry run)"
		}
		return fmt.Sprintf("%q → %q in %d dirs%s", truncate(search, 25), truncate(replace, 25), dirs, suffix)

	case "read":
		file := shortPath(inputStr(input, "file"))
		start, hasStart := input["start"].(float64)
		end, hasEnd := input["end"].(float64)
		if hasStart && hasEnd {
			return fmt.Sprintf("%s:%d–%d", file, int(start), int(end))
		}
		if hasStart {
			return fmt.Sprintf("%s:%d–", file, int(start))
		}
		return file

	case "write":
		file := shortPath(inputStr(input, "file"))
		content, _ := input["content"].(string)
		lines := strings.Count(content, "\n") + 1
		return fmt.Sprintf("%s (%d lines)", file, lines)

	case "bash":
		cmd, _ := input["command"].(string)
		return truncate(cmd, 60)

	case "sig":
		return shortPath(inputStr(input, "file"))

	case "stump":
		dir := shortPath(inputStr(input, "dir"))
		if depth, ok := input["depth"].(float64); ok && depth > 0 {
			return fmt.Sprintf("%s (depth %d)", dir, int(depth))
		}
		return dir

	case "cleanDiff":
		if ref, ok := input["ref"].(string); ok && ref != "" {
			return ref
		}
		if staged, ok := input["staged"].(bool); ok && staged {
			return "staged"
		}
		return "working tree"

	case "splice":
		file := shortPath(inputStr(input, "file"))
		if line, ok := input["line"].(float64); ok {
			return fmt.Sprintf("%s at line %d", file, int(line))
		}
		return file

	case "split":
		file := shortPath(inputStr(input, "file"))
		if line, ok := input["line"].(float64); ok {
			return fmt.Sprintf("%s at line %d", file, int(line))
		}
		return file

	case "delete":
		return shortPath(inputStr(input, "path"))

	case "notab":
		return shortPath(inputStr(input, "file"))

	case "tabcount":
		return shortPath(inputStr(input, "file"))

	case "utf8":
		return shortPath(inputStr(input, "file"))

	case "imports":
		dir := shortPath(inputStr(input, "dir"))
		if recursive, ok := input["recursive"].(bool); ok && recursive {
			return dir + " (recursive)"
		}
		return dir

	case "errs":
		if format, ok := input["format"].(string); ok && format != "" {
			return fmt.Sprintf("(%s)", format)
		}
		return ""

	case "conflicts":
		return shortPath(inputStr(input, "file"))

	case "transform":
		if exec, ok := input["exec"].(string); ok && exec != "" {
			return fmt.Sprintf("exec: %s", truncate(exec, 50))
		}
		if file, ok := input["file"].(string); ok && file != "" {
			return fmt.Sprintf("file: %s", shortPath(file))
		}
		return ""

	default:
		// MCP tools or unknown — show first string param value
		for _, v := range input {
			if s, ok := v.(string); ok && s != "" {
				return truncate(s, 50)
			}
		}
		return ""
	}
}

// formatToolLine formats a complete single-line tool display.
// Success: "  ● name summary → result (duration)"
// Error:   "  ✗ name summary (duration)\n      error message"
func formatToolLine(name, inputSum, resultSum, errMsg string, elapsed time.Duration, isError bool) string {
	dur := formatDuration(elapsed)
	if isError {
		line := fmt.Sprintf("  ✗ %s %s (%s)", name, inputSum, dur)
		if errMsg != "" {
			line += "\n" + fmt.Sprintf("      %s", truncate(errMsg, 120))
		}
		return line
	}
	if resultSum != "" {
		return fmt.Sprintf("  ● %s %s → %s (%s)", name, inputSum, resultSum, dur)
	}
	return fmt.Sprintf("  ● %s %s (%s)", name, inputSum, dur)
}

// prettyOutput attempts to produce a compact human-readable summary of tool output.
// Recognizes common JSON shapes from precon tools and formats them specially.
// Falls back to truncated raw output.
func prettyOutput(output string, maxLen int) string {
	if output == "" {
		return ""
	}

	// Try to parse as JSON object for known tool output shapes
	var obj map[string]any
	if err := json.Unmarshal([]byte(output), &obj); err != nil {
		// Not JSON — return truncated raw
		return truncate(output, maxLen)
	}

	// stump: {"root", "stats": {"dirs", "files"}, "tree": [...]}
	if root, ok := obj["root"].(string); ok {
		if stats, ok := obj["stats"].(map[string]any); ok {
			dirs, _ := stats["dirs"].(float64)
			files, _ := stats["files"].(float64)
			return fmt.Sprintf("%s: %d dirs, %d files", root, int(dirs), int(files))
		}
	}

	// cleanDiff: {"summary": {"files_changed", "insertions", "deletions"}}
	if summaryObj, ok := obj["summary"].(map[string]any); ok {
		if _, hasFChanged := summaryObj["files_changed"]; hasFChanged {
			fc, _ := summaryObj["files_changed"].(float64)
			ins, _ := summaryObj["insertions"].(float64)
			del, _ := summaryObj["deletions"].(float64)
			return fmt.Sprintf("%d files changed, +%d -%d", int(fc), int(ins), int(del))
		}
		// imports: {"summary": {"total_files", "total_imports"}}
		if tf, ok := summaryObj["total_files"].(float64); ok {
			ti, _ := summaryObj["total_imports"].(float64)
			return fmt.Sprintf("%d imports across %d files", int(ti), int(tf))
		}
	}

	// checkfor: {"matches": [...]} or {"directories": [...]}
	if matches, ok := obj["matches"].([]any); ok {
		return fmt.Sprintf("%d matches", len(matches))
	}
	if dirs, ok := obj["directories"].([]any); ok {
		total := 0
		for _, d := range dirs {
			if dm, ok := d.(map[string]any); ok {
				if n, ok := dm["matches_found"].(float64); ok {
					total += int(n)
				}
			}
		}
		return fmt.Sprintf("%d matches", total)
	}

	// repfor: {"files_modified", "replacements"} or {"summary": "..."}
	if n, ok := obj["files_modified"].(float64); ok {
		replacements, _ := obj["replacements"].(float64)
		return fmt.Sprintf("%d replacements in %d files", int(replacements), int(n))
	}

	// sig: {"file", "functions", "types"}
	if _, hasFunctions := obj["functions"]; hasFunctions {
		nFuncs, nTypes := 0, 0
		if fns, ok := obj["functions"].([]any); ok {
			nFuncs = len(fns)
		}
		if tps, ok := obj["types"].([]any); ok {
			nTypes = len(tps)
		}
		return fmt.Sprintf("%d functions, %d types", nFuncs, nTypes)
	}

	// errs: {"count", "files", "format"}
	if count, ok := obj["count"].(float64); ok {
		if nFiles, ok := obj["files"].(float64); ok {
			format, _ := obj["format"].(string)
			if format != "" {
				return fmt.Sprintf("%d errors in %d files (%s)", int(count), int(nFiles), format)
			}
			return fmt.Sprintf("%d errors in %d files", int(count), int(nFiles))
		}
	}

	// delete: {"original_path", "trash_path"}
	if _, ok := obj["trash_path"].(string); ok {
		origPath, _ := obj["original_path"].(string)
		return fmt.Sprintf("trashed %s", shortPath(origPath))
	}

	// conflicts: {"total", "has_diff3"}
	if total, ok := obj["total"].(float64); ok {
		if _, hasHasDiff3 := obj["has_diff3"]; hasHasDiff3 {
			return fmt.Sprintf("%d conflicts", int(total))
		}
	}

	// notab: {"replacements", "lines_affected"}
	if replacements, ok := obj["replacements"].(float64); ok {
		if lines, ok := obj["lines_affected"].(float64); ok {
			if int(replacements) == 0 {
				return "no changes"
			}
			return fmt.Sprintf("%d replacements on %d lines", int(replacements), int(lines))
		}
	}

	// tabcount: {"tabs", "spaces"}
	if tabs, ok := obj["tabs"].(float64); ok {
		if spaces, ok := obj["spaces"].(float64); ok {
			return fmt.Sprintf("%d tabs, %d spaces", int(tabs), int(spaces))
		}
	}

	// read/write: {"status", "file"}
	if status, ok := obj["status"].(string); ok {
		if file, ok := obj["file"].(string); ok {
			return fmt.Sprintf("%s: %s", shortPath(file), status)
		}
		return status
	}

	// Generic: "summary" string
	if summary, ok := obj["summary"].(string); ok && summary != "" {
		return truncate(summary, maxLen)
	}

	// Fallback: truncated raw output (no pretty-printed JSON dump)
	return truncate(output, maxLen)
}

// inputStr extracts a string value from an input map, returning "" if missing.
func inputStr(input map[string]any, key string) string {
	s, _ := input[key].(string)
	return s
}

// countArray returns the length of a value that should be a []any, or 0.
func countArray(v any) int {
	if arr, ok := v.([]any); ok {
		return len(arr)
	}
	return 0
}

// shortPath returns the last 2 path components for compact display.
// "/Users/home/Documents/Code/Go_dev/precon/internal/repl/send.go" → "repl/send.go"
func shortPath(path string) string {
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	parent := filepath.Base(dir)
	if parent == "." || parent == "/" {
		return base
	}
	return parent + "/" + base
}
