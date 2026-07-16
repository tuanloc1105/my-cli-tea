package ui

import (
	"check-folder-size/internal/scanner"
	"fmt"
	"io"
	"strings"
)

type FormatResult struct {
	Size  float64
	Unit  string
	Color int
}

// color formats text with ANSI colors
func color(msg string, bg int) string {
	return fmt.Sprintf("\033[%dm\033[1;30m %s \033[0m", bg, msg)
}

// formatSize converts bytes to human readable format
func formatSize(size int64) FormatResult {
	if size == 0 {
		return FormatResult{0, "bytes", 42}
	}

	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	unitIndex := 0
	sizeFloat := float64(size)

	for sizeFloat >= 1024 && unitIndex < len(units)-1 {
		sizeFloat /= 1024
		unitIndex++
	}

	// Color based on size: green for small, yellow for medium, red for large
	var msgColor int
	if unitIndex <= 1 { // bytes, KB
		msgColor = 42 // green
	} else if unitIndex <= 2 { // MB
		msgColor = 43 // yellow
	} else { // GB, TB
		msgColor = 41 // red
	}

	return FormatResult{sizeFloat, units[unitIndex], msgColor}
}

// PrintResults displays pre-sorted folder analysis results.
func PrintResults(writer io.Writer, items []scanner.ItemInfo, parentFolder string, sizeMode scanner.SizeMode) {
	if len(items) == 0 {
		fmt.Fprintf(writer, "No accessible folders or files found (%s size).\n", strings.ToLower(metricLabel(sizeMode)))
		return
	}

	// Calculate total size
	var totalSize int64
	for _, item := range items {
		totalSize += item.Size
	}
	totalFormatted := formatSize(totalSize)

	// Print header
	fmt.Fprintf(writer, "\n%s\n", strings.Repeat("=", 80))
	fmt.Fprintf(writer, "📁 Parent Folder: %s\n", parentFolder)
	fmt.Fprintf(writer, "📊 Total %s Size: %.2f %s\n", metricLabel(sizeMode), totalFormatted.Size, color(totalFormatted.Unit, totalFormatted.Color))
	fmt.Fprintf(writer, "📈 Items Found: %d\n", len(items))
	fmt.Fprintf(writer, "%s\n", strings.Repeat("=", 80))

	// Print table header
	const unitColWidth = 7 // max visible width: " bytes " = 7
	const typeColWidth = 9 // "directory" = 9
	fmt.Fprintf(writer, "%10s  %-*s  %-*s  %s\n", "Size", unitColWidth, "Unit", typeColWidth, "Type", "Name")
	fmt.Fprintf(writer, "%10s  %-*s  %-*s  %s\n", "----", unitColWidth, "----", typeColWidth, "----", "----")

	// Print items
	for _, item := range items {
		formatted := formatSize(item.Size)
		sizeStr := fmt.Sprintf("%10.2f", formatted.Size)
		unitStr := color(formatted.Unit, formatted.Color)

		// Pad after colored unit to align Name column
		// color() adds 1 space on each side, so visible width = len(unit) + 2
		padCount := unitColWidth - (len(formatted.Unit) + 2)
		padding := ""
		if padCount > 0 {
			padding = strings.Repeat(" ", padCount)
		}

		fmt.Fprintf(writer, "%s  %s%s  %-*s  %s\n", sizeStr, unitStr, padding, typeColWidth, item.Type, item.Name)
	}

	fmt.Fprintln(writer, strings.Repeat("-", 80))
}

func metricLabel(sizeMode scanner.SizeMode) string {
	if sizeMode == scanner.SizeModeLogical {
		return "Logical"
	}
	return "Allocated"
}
