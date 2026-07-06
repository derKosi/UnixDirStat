package main

import (
	"hash/fnv"
	"strings"
)

// WinDirStat-style color palette for file extensions.
// Deterministic: same extension always gets the same color.
var extColorMap = map[string]string{
	// Archives
	".zip": "#E06040", ".tar": "#E06040", ".gz": "#E06040", ".bz2": "#E06040",
	".xz": "#E06040", ".7z": "#E06040", ".rar": "#E06040", ".zst": "#E06040",

	// Images
	".jpg": "#A040E0", ".jpeg": "#A040E0", ".png": "#A040E0", ".gif": "#A040E0",
	".svg": "#A040E0", ".webp": "#A040E0", ".bmp": "#A040E0", ".ico": "#A040E0",

	// Video
	".mp4": "#40A0E0", ".mkv": "#40A0E0", ".avi": "#40A0E0", ".mov": "#40A0E0",
	".wmv": "#40A0E0", ".webm": "#40A0E0", ".flv": "#40A0E0", ".m4v": "#40A0E0",

	// Audio
	".mp3": "#40E0A0", ".wav": "#40E0A0", ".flac": "#40E0A0", ".ogg": "#40E0A0",
	".aac": "#40E0A0", ".wma": "#40E0A0", ".m4a": "#40E0A0",

	// Documents
	".pdf": "#E0E040", ".doc": "#E0E040", ".docx": "#E0E040", ".txt": "#E0E040",
	".rtf": "#E0E040", ".odt": "#E0E040", ".xls": "#E0E040", ".xlsx": "#E0E040",
	".csv": "#E0E040", ".pptx": "#E0E040",

	// Code
	".go": "#00ADD8", ".rs": "#DEA584", ".py": "#FFD43B", ".js": "#F7DF1E",
	".ts": "#3178C6", ".cs": "#68217A", ".java": "#B07219", ".c": "#555555",
	".cpp": "#F34B7D", ".h": "#555555", ".rb": "#CC342D", ".php": "#4F5D95",

	// Web
	".html": "#E44D26", ".css": "#563D7C", ".scss": "#CD6799",

	// Data/Config
	".json": "#6B6B6B", ".xml": "#6B6B6B", ".yaml": "#6B6B6B", ".yml": "#6B6B6B",
	".toml": "#6B6B6B", ".ini": "#6B6B6B", ".cfg": "#6B6B6B", ".conf": "#6B6B6B",

	// Database
	".db": "#D4A017", ".sqlite": "#D4A017", ".sql": "#D4A017",

	// Executables
	".exe": "#FF4444", ".dll": "#FF4444", ".so": "#FF4444", ".bin": "#FF4444",
	".app": "#FF4444",

	// Logs
	".log": "#888888",

	// Fonts
	".ttf": "#C8A0D0", ".otf": "#C8A0D0", ".woff": "#C8A0D0", ".woff2": "#C8A0D0",
}

// Fallback ANSI colors for terminals without truecolor.
var fallbackColors = []string{
	"#f7768e", "#ff9e64", "#e0af68", "#9ece6a",
	"#73daca", "#2ac3de", "#7dcfff", "#7aa2f7",
	"#bb9af7", "#d4a017", "#ff007c", "#f7768e",
	"#b4f9f8", "#ff9e64", "#2ac3de", "#9ece6a",
	"#e0af68", "#7dcfff", "#bb9af7", "#f7768e",
}

// ExtColor returns a hex color for a file extension.
func ExtColor(ext string) string {
	ext = strings.ToLower(ext)
	if c, ok := extColorMap[ext]; ok {
		return c
	}
	// Deterministic fallback via hash
	h := fnv.New32a()
	h.Write([]byte(ext))
	idx := h.Sum32() % uint32(len(fallbackColors))
	return fallbackColors[idx]
}
