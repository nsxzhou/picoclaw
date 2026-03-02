package agent

import "fmt"

func formatAttachmentSizeHuman(sizeBytes int64) string {
	if sizeBytes < 1024 {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	if sizeBytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(sizeBytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(sizeBytes)/(1024*1024))
}
