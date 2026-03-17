package repl

import (
	"encoding/json"
	"fmt"
)

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
		file, _ := obj["file"].(string)
		nFuncs, nTypes := 0, 0
		if fns, ok := obj["functions"].([]any); ok {
			nFuncs = len(fns)
		}
		if tps, ok := obj["types"].([]any); ok {
			nTypes = len(tps)
		}
		return fmt.Sprintf("%s: %d functions, %d types", file, nFuncs, nTypes)
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
		return fmt.Sprintf("moved %s to Trash", origPath)
	}

	// conflicts: {"total", "has_diff3"}
	if total, ok := obj["total"].(float64); ok {
		if _, hasHasDiff3 := obj["has_diff3"]; hasHasDiff3 {
			return fmt.Sprintf("%d conflicts", int(total))
		}
	}

	// Generic: "summary" string
	if summary, ok := obj["summary"].(string); ok && summary != "" {
		return truncate(summary, maxLen)
	}

	// Generic: "status" string
	if status, ok := obj["status"].(string); ok {
		if file, ok := obj["file"].(string); ok {
			return fmt.Sprintf("%s: %s", file, status)
		}
		return status
	}

	// Fallback: pretty-printed JSON
	pretty, err := json.MarshalIndent(obj, "    ", "  ")
	if err != nil {
		return truncate(output, maxLen)
	}
	return truncate(string(pretty), maxLen)
}
