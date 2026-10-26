package render

import "github.com/dpologdvinity/depwhy/internal/size"

// FormatSize formats a byte count into a human-readable string with units.
func FormatSize(bytes int64) string {
	return size.Format(bytes)
}
