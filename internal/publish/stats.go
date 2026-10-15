package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteStatsFile writes the posting stats as a dotenv file for downstream CI
// jobs. It is written on every exit path, zero-filled when nothing was posted.
func WriteStatsFile(path string, stats *Stats) error {
	runStatus := stats.RunStatus
	if runStatus == "" {
		runStatus = "completed"
	}
	lines := []string{
		fmt.Sprintf("OCR_COMMENTS_TOTAL=%d", stats.Total),
		fmt.Sprintf("OCR_COMMENTS_INLINE=%d", stats.Inline),
		fmt.Sprintf("OCR_COMMENTS_SUMMARY=%d", stats.Summary),
		fmt.Sprintf("OCR_COMMENTS_ROUTED=%d", stats.Routed),
		fmt.Sprintf("OCR_COMMENTS_SKIPPED=%d", stats.Skipped),
		fmt.Sprintf("OCR_COMMENTS_FAILED=%d", stats.Failed),
		fmt.Sprintf("OCR_FINDINGS_RESOLVED=%d", stats.Resolved),
		fmt.Sprintf("OCR_SUMMARY_URL=%s", stats.SummaryURL),
		fmt.Sprintf("OCR_RUN_STATUS=%s", runStatus),
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create stats dir: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("write stats file: %w", err)
	}
	return nil
}
