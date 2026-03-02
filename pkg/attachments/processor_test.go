package attachments

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	godocx "github.com/gomutex/godocx"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/xuri/excelize/v2"
)

func TestProcessor_ProcessTXT(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(filePath, []byte("hello\nworld"), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments, errs := NewProcessor(ProcessorOptions{}).Process([]string{filePath})
	if len(errs) != 0 {
		t.Fatalf("len(errs) = %d, want 0", len(errs))
	}
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if attachments[0].TextContent != "hello\nworld" {
		t.Fatalf("TextContent = %q, want %q", attachments[0].TextContent, "hello\nworld")
	}
}

func TestProcessor_ProcessDOCX(t *testing.T) {
	filePath := createDOCXFixture(t, "sample.docx", []string{
		"Hello",
		"DOCX",
	})

	attachments, errs := Process([]string{filePath})
	if len(errs) != 0 {
		t.Fatalf("len(errs) = %d, want 0", len(errs))
	}
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if attachments[0].TextContent != "Hello\nDOCX" {
		t.Fatalf("TextContent = %q, want %q", attachments[0].TextContent, "Hello\nDOCX")
	}
}

func TestProcessor_ProcessXLSX(t *testing.T) {
	filePath := createXLSXFixture(t, "sample.xlsx", []xlsxSheetFixture{
		{
			Name: "Sheet1",
			Cells: map[string]any{
				"A1": "name",
				"B1": "Alice",
			},
		},
	})

	attachments, errs := Process([]string{filePath})
	if len(errs) != 0 {
		t.Fatalf("len(errs) = %d, want 0", len(errs))
	}
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if !strings.Contains(attachments[0].TextContent, "[sheet: Sheet1]") ||
		!strings.Contains(attachments[0].TextContent, "A1=name") ||
		!strings.Contains(attachments[0].TextContent, "B1=Alice") {
		t.Fatalf("TextContent = %q, want contains parsed cells", attachments[0].TextContent)
	}
}

func TestProcessor_ProcessXLSX_MultiSheet(t *testing.T) {
	filePath := createXLSXFixture(t, "multi.xlsx", []xlsxSheetFixture{
		{
			Name: "Sheet1",
			Cells: map[string]any{
				"A1": "name",
				"B1": "Alice",
			},
		},
		{
			Name: "Data",
			Cells: map[string]any{
				"A1": "city",
				"B1": "Shenzhen",
			},
		},
	})

	attachments, errs := Process([]string{filePath})
	if len(errs) != 0 {
		t.Fatalf("len(errs) = %d, want 0", len(errs))
	}
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}

	text := attachments[0].TextContent
	if !strings.Contains(text, "[sheet: Sheet1]") || !strings.Contains(text, "[sheet: Data]") {
		t.Fatalf("TextContent = %q, want contains all sheet headers", text)
	}
}

func TestProcessor_ProcessPDF(t *testing.T) {
	// Hand-crafted PDFs without xref tables are rejected by ledongthuc/pdf.
	// This test verifies the error path produces a meaningful parse_failed error.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.pdf")
	pdfContent := "%PDF-1.4\n1 0 obj\n<<>>\nstream\nBT\n(Hello PDF) Tj\nET\nendstream\nendobj\n%%EOF"
	if err := os.WriteFile(filePath, []byte(pdfContent), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments, errs := Process([]string{filePath})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].Code != "parse_failed" {
		t.Fatalf("errs[0].Code = %q, want %q", errs[0].Code, "parse_failed")
	}
}

func TestProcessor_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(filePath, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}

	processor := NewProcessor(ProcessorOptions{MaxFileSizeBytes: 4})
	attachments, errs := processor.Process([]string{filePath})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].Code != "file_too_large" {
		t.Fatalf("errs[0].Code = %q, want %q", errs[0].Code, "file_too_large")
	}
}

func TestProcessor_UnsupportedAudio(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "voice.wav")
	if err := os.WriteFile(filePath, []byte("RIFFxxxxWAVEfmt "), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments, errs := Process([]string{filePath})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].Code != "audio_not_supported" {
		t.Fatalf("errs[0].Code = %q, want %q", errs[0].Code, "audio_not_supported")
	}
}

func TestInferMediaTypeFromName(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     string
	}{
		{name: "pdf", fileName: "report.pdf", want: "application/pdf"},
		{name: "doc", fileName: "report.doc", want: "application/msword"},
		{name: "docx", fileName: "report.docx", want: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{name: "xlsx", fileName: "data.xlsx", want: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{name: "pptx", fileName: "slides.pptx", want: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{name: "csv", fileName: "table.csv", want: "text/csv"},
		{name: "png", fileName: "image.png", want: "image/png"},
		{name: "unknown", fileName: "archive.unknownext", want: "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferMediaTypeFromName(tt.fileName)
			if got != tt.want {
				t.Fatalf("InferMediaTypeFromName(%q) = %q, want %q", tt.fileName, got, tt.want)
			}
		})
	}
}

func TestInferAttachmentKindFromName(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     bus.AttachmentKind
	}{
		{name: "image", fileName: "photo.jpg", want: bus.AttachmentKindImage},
		{name: "audio", fileName: "voice.mp3", want: bus.AttachmentKindAudio},
		{name: "video", fileName: "movie.mp4", want: bus.AttachmentKindVideo},
		{name: "document", fileName: "report.pdf", want: bus.AttachmentKindDocument},
		{name: "unknown-ext-default-document", fileName: "blob.abcxyz", want: bus.AttachmentKindDocument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferAttachmentKindFromName(tt.fileName)
			if got != tt.want {
				t.Fatalf("InferAttachmentKindFromName(%q) = %q, want %q", tt.fileName, got, tt.want)
			}
		})
	}
}

func TestProcessor_ProcessDOCX_ParseFailed(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "broken.docx")
	if err := os.WriteFile(filePath, []byte("not a valid docx"), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments, errs := Process([]string{filePath})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].Code != "parse_failed" {
		t.Fatalf("errs[0].Code = %q, want %q", errs[0].Code, "parse_failed")
	}
}

func createDOCXFixture(t *testing.T, name string, paragraphs []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	document, err := godocx.NewDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, paragraph := range paragraphs {
		document.AddParagraph(paragraph)
	}
	if err := document.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	return path
}

type xlsxSheetFixture struct {
	Name  string
	Cells map[string]any
}

func createXLSXFixture(t *testing.T, name string, sheets []xlsxSheetFixture) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	workbook := excelize.NewFile()
	defer func() {
		_ = workbook.Close()
	}()

	defaultSheet := workbook.GetSheetName(workbook.GetActiveSheetIndex())
	for index, sheet := range sheets {
		if sheet.Name == "" {
			t.Fatal("sheet name cannot be empty")
		}

		targetSheet := sheet.Name
		switch {
		case index == 0 && defaultSheet != targetSheet:
			if err := workbook.SetSheetName(defaultSheet, targetSheet); err != nil {
				t.Fatal(err)
			}
		case index > 0:
			if _, err := workbook.NewSheet(targetSheet); err != nil {
				t.Fatal(err)
			}
		}

		for cellRef, value := range sheet.Cells {
			if err := workbook.SetCellValue(targetSheet, cellRef, value); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := workbook.SaveAs(path); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestProcessor_ProcessPDF_MalformedPDF(t *testing.T) {
	// A hand-crafted PDF with invalid compressed data and no xref table
	// should produce a parse_failed error, not a panic.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "bad.pdf")
	content := "%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\nnot valid zlib\nendstream\nendobj\n%%EOF"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	attachments, errs := Process([]string{filePath})
	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0].Code != "parse_failed" {
		t.Fatalf("errs[0].Code = %q, want %q", errs[0].Code, "parse_failed")
	}
	if !strings.Contains(errs[0].UserMessage, "could not be parsed") {
		t.Fatalf("UserMessage = %q, want contains 'could not be parsed'", errs[0].UserMessage)
	}
}

func TestExtractPDFText_InvalidFile(t *testing.T) {
	// extractPDFText should return an error for non-PDF files
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-pdf.pdf")
	if err := os.WriteFile(filePath, []byte("this is not a PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := extractPDFText(filePath, defaultMaxTextChars)
	if err == nil {
		t.Fatal("expected error for non-PDF file")
	}
}

func TestExtractPDFText_NonexistentFile(t *testing.T) {
	_, err := extractPDFText("/tmp/nonexistent-pdf-test-file.pdf", defaultMaxTextChars)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExtractPDFText_RespectsMaxTextChars(t *testing.T) {
	filePath := createPDFFixture(t, "long.pdf", strings.Repeat("A", 240))

	shortText, err := extractPDFText(filePath, 10)
	if err != nil {
		t.Fatalf("extractPDFText() with short limit failed: %v", err)
	}
	longText, err := extractPDFText(filePath, 200)
	if err != nil {
		t.Fatalf("extractPDFText() with long limit failed: %v", err)
	}

	if len(shortText) >= len(longText) {
		t.Fatalf("expected short-limit text to be smaller, got short=%d long=%d", len(shortText), len(longText))
	}
	if len(shortText) > 40 {
		t.Fatalf("short-limit text too long: got %d, want <= 40", len(shortText))
	}
}

func TestSummarizeParseError(t *testing.T) {
	err := fmt.Errorf("no extractable text found in PDF")
	msg := summarizeParseError("test.pdf", "application/pdf", err)
	if !strings.Contains(msg, "test.pdf") {
		t.Fatalf("message should contain filename: %q", msg)
	}
	if !strings.Contains(msg, "no extractable text found in PDF") {
		t.Fatalf("message should contain error reason: %q", msg)
	}
	if !strings.Contains(msg, "application/pdf") {
		t.Fatalf("message should contain media type: %q", msg)
	}
}

func createPDFFixture(t *testing.T, name string, text string) string {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 6)

	write := func(s string) {
		_, _ = buf.WriteString(s)
	}

	write("%PDF-1.4\n")

	offsets[1] = buf.Len()
	write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	offsets[2] = buf.Len()
	write("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	offsets[3] = buf.Len()
	write(
		"3 0 obj\n" +
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\n" +
			"endobj\n",
	)

	content := "BT\n/F1 12 Tf\n72 720 Td\n(" + escapePDFText(text) + ") Tj\nET\n"
	offsets[4] = buf.Len()
	write(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(content), content))

	offsets[5] = buf.Len()
	write("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	startXRef := buf.Len()
	write("xref\n0 6\n")
	write("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		write(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	write("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	write(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", startXRef))

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func escapePDFText(text string) string {
	text = strings.ReplaceAll(text, "\\", "\\\\")
	text = strings.ReplaceAll(text, "(", "\\(")
	text = strings.ReplaceAll(text, ")", "\\)")
	return text
}
