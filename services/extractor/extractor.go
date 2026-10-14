package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/otiai10/gosseract/v2"
)

func extractText(ctx context.Context, inputPath, contentType string) (string, error) {
	switch contentType {
	case "application/pdf":
		return extractPDF(ctx, inputPath)
	case "image/png", "image/jpeg", "image/tiff", "image/bmp":
		return extractImage(ctx, inputPath)
	default:
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}
}

func extractPDF(ctx context.Context, inputPath string) (string, error) {
	// #nosec G204 -- executable is fixed and inputPath is a temporary file created by storage.Download.
	output, err := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", inputPath, "-").Output()
	if err != nil {
		return "", fmt.Errorf("extract PDF text: %w", err)
	}
	if text := strings.TrimSpace(string(output)); text != "" {
		return string(output), nil
	}

	pageCount, err := pdfPageCount(ctx, inputPath)
	if err != nil {
		return "", err
	}
	client := gosseract.NewClient()
	defer client.Close()
	tempDir, err := os.MkdirTemp("", "cv-pdf-*")
	if err != nil {
		return "", fmt.Errorf("create PDF image directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	var text strings.Builder
	for page := 1; page <= pageCount; page++ {
		prefix := filepath.Join(tempDir, fmt.Sprintf("page-%04d", page))
		pageArg := strconv.Itoa(page)
		// #nosec G204 -- executable is fixed; inputPath and prefix are temporary files under generated temp directories.
		cmd := exec.CommandContext(ctx, "pdftoppm", "-f", pageArg, "-l", pageArg, "-singlefile", "-png", "-r", "200", inputPath, prefix)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("render PDF page %d: %w: %s", page, err, strings.TrimSpace(string(output)))
		}
		imagePath := prefix + ".png"
		pageText, err := recognizeImage(client, imagePath)
		if err != nil {
			return "", fmt.Errorf("recognize PDF page %d: %w", page, err)
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(pageText)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("no text found in PDF")
	}
	return text.String(), nil
}

func pdfPageCount(ctx context.Context, inputPath string) (int, error) {
	// #nosec G204 -- executable is fixed and inputPath is a temporary file created by storage.Download.
	output, err := exec.CommandContext(ctx, "pdfinfo", inputPath).Output()
	if err != nil {
		return 0, fmt.Errorf("read PDF metadata: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		name, value, found := strings.Cut(line, ":")
		if found && strings.TrimSpace(name) == "Pages" {
			pages, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || pages < 1 {
				return 0, fmt.Errorf("invalid PDF page count %q", strings.TrimSpace(value))
			}
			return pages, nil
		}
	}
	return 0, fmt.Errorf("PDF metadata did not contain a page count")
}

func extractImage(ctx context.Context, inputPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	client := gosseract.NewClient()
	defer client.Close()
	return recognizeImage(client, inputPath)
}

func recognizeImage(client *gosseract.Client, imagePath string) (string, error) {
	if err := client.SetImage(imagePath); err != nil {
		return "", fmt.Errorf("set OCR image: %w", err)
	}
	text, err := client.Text()
	if err != nil {
		return "", fmt.Errorf("run OCR: %w", err)
	}
	return text, nil
}
