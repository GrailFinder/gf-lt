package rag

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ledongthuc/pdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

func ExtractText(fpath string) (string, error) {
	ext := strings.ToLower(path.Ext(fpath))
	switch ext {
	case ".txt":
		return extractTextFromFile(fpath)
	case ".md", ".markdown":
		return extractTextFromMarkdown(fpath)
	case ".html", ".htm":
		return extractTextFromHtmlFile(fpath)
	case ".epub":
		return extractTextFromEpub(fpath)
	case ".pdf":
		return extractTextFromPdf(fpath)
	default:
		return "", fmt.Errorf("unsupported file format: %s", ext)
	}
}

func extractTextFromFile(fpath string) (string, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func extractTextFromHtmlFile(fpath string) (string, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return "", err
	}
	return extractTextFromHtmlContent(data)
}

// non utf-8 encoding?
func extractTextFromHtmlContent(data []byte) (string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	// Remove script and style tags
	doc.Find("script, style, noscript").Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})
	// Get text and clean it
	text := doc.Text()
	// Collapse all whitespace (newlines, tabs, multiple spaces) into single spaces
	cleaned := strings.Join(strings.Fields(text), " ")
	return cleaned, nil
}

func extractTextFromMarkdown(fpath string) (string, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return "", err
	}
	// Convert markdown to HTML
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()), // allow raw HTML if needed
	)
	var buf bytes.Buffer
	if err := md.Convert(data, &buf); err != nil {
		return "", err
	}
	// Now extract text from the resulting HTML (using goquery or similar)
	return extractTextFromHtmlContent(buf.Bytes())
}

func extractTextFromEpub(fpath string) (string, error) {
	r, err := zip.OpenReader(fpath)
	if err != nil {
		return "", fmt.Errorf("failed to open epub: %w", err)
	}
	defer r.Close()
	var sb strings.Builder
	for _, f := range r.File {
		ext := strings.ToLower(path.Ext(f.Name))
		if ext != ".xhtml" && ext != ".html" && ext != ".htm" && ext != ".xml" {
			continue
		}

		// Skip manifest, toc, ncx files - they don't contain book content
		nameLower := strings.ToLower(f.Name)
		if strings.Contains(nameLower, "toc") || strings.Contains(nameLower, "nav") ||
			strings.Contains(nameLower, "manifest") || strings.Contains(nameLower, ".opf") ||
			strings.HasSuffix(nameLower, ".ncx") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(f.Name)
		sb.WriteString("\n")

		buf, readErr := io.ReadAll(rc)
		rc.Close()
		if readErr == nil {
			sb.WriteString(stripHTML(string(buf)))
		}
	}
	if sb.Len() == 0 {
		return "", errors.New("no content extracted from epub")
	}
	return sb.String(), nil
}

func stripHTML(html string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range html {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

func extractTextFromPdf(fpath string) (string, error) {
	_, err := exec.LookPath("pdftotext")
	if err == nil {
		out, err := exec.Command("pdftotext", "-layout", fpath, "-").Output()
		if err == nil && len(out) > 0 {
			return string(out), nil
		}
	}
	return extractTextFromPdfPureGo(fpath)
}

func extractTextFromPdfPureGo(fpath string) (string, error) {
	df, r, err := pdf.Open(fpath)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf: %w", err)
	}
	defer df.Close()
	textReader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract text from pdf: %w", err)
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, textReader)
	if err != nil {
		return "", fmt.Errorf("failed to read pdf text: %w", err)
	}
	return buf.String(), nil
}
