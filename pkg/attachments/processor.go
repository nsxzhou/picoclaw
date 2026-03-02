package attachments

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	godocx "github.com/gomutex/godocx"
	"github.com/gomutex/godocx/wml/ctypes"
	"github.com/ledongthuc/pdf"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/xuri/excelize/v2"
)

const (
	defaultMaxFileSizeBytes = int64(4 * 1024 * 1024)
	defaultMaxTextChars     = 60000
)

type ProcessorOptions struct {
	MaxFileSizeBytes int64
	MaxTextChars     int
}

type Processor struct {
	maxFileSizeBytes int64
	maxTextChars     int
}

func NewProcessor(opts ProcessorOptions) *Processor {
	maxFileSizeBytes := opts.MaxFileSizeBytes
	if maxFileSizeBytes <= 0 {
		maxFileSizeBytes = defaultMaxFileSizeBytes
	}

	maxTextChars := opts.MaxTextChars
	if maxTextChars <= 0 {
		maxTextChars = defaultMaxTextChars
	}

	return &Processor{
		maxFileSizeBytes: maxFileSizeBytes,
		maxTextChars:     maxTextChars,
	}
}

func Process(paths []string) ([]bus.Attachment, []bus.AttachmentError) {
	return NewProcessor(ProcessorOptions{}).Process(paths)
}

func (p *Processor) Process(paths []string) ([]bus.Attachment, []bus.AttachmentError) {
	if len(paths) == 0 {
		return nil, nil
	}

	attachments := make([]bus.Attachment, 0, len(paths))
	errors := make([]bus.AttachmentError, 0)

	for _, path := range paths {
		if path == "" {
			continue
		}

		attachment, parseErr := p.processOne(path)
		if attachment != nil {
			attachments = append(attachments, *attachment)
		}
		if parseErr != nil {
			errors = append(errors, *parseErr)
		}
	}

	if len(attachments) == 0 {
		attachments = nil
	}
	if len(errors) == 0 {
		errors = nil
	}

	return attachments, errors
}

func (p *Processor) processOne(path string) (*bus.Attachment, *bus.AttachmentError) {
	info, err := os.Stat(path)
	if err != nil {
		name := filepath.Base(path)
		if name == "." || name == string(filepath.Separator) {
			name = path
		}
		return nil, buildError(name, "file_unreadable", err.Error(),
			fmt.Sprintf("Attachment %q was received but cannot be read.", name))
	}

	name := info.Name()
	ext := strings.ToLower(filepath.Ext(name))
	mediaType := detectMediaType(path, ext)
	kind := classifyKind(mediaType, ext)

	attachment := &bus.Attachment{
		Name:      name,
		MediaType: mediaType,
		SizeBytes: info.Size(),
		LocalPath: path,
		Kind:      kind,
	}

	switch {
	case kind == bus.AttachmentKindImage:
		return attachment, nil
	case kind == bus.AttachmentKindAudio:
		return attachment, buildError(name, "audio_not_supported", "",
			fmt.Sprintf("Audio attachment %q was received but direct audio understanding is not supported in this path.", name))
	case kind == bus.AttachmentKindVideo:
		return attachment, buildError(name, "video_not_supported", "",
			fmt.Sprintf("Video attachment %q was received but direct video understanding is not supported in this path.", name))
	}

	docType := detectDocumentType(mediaType, ext)
	if docType == docTypeUnsupported {
		return attachment, buildError(name, "unsupported_type", mediaType,
			fmt.Sprintf("Attachment %q type (%s) is not supported for content understanding.", name, mediaType))
	}

	if info.Size() > p.maxFileSizeBytes {
		return attachment, buildError(name, "file_too_large", fmt.Sprintf("%d bytes", info.Size()),
			fmt.Sprintf("Attachment %q is too large to parse. Please upload a smaller file.", name))
	}

	text, err := p.extractText(path, docType)
	if err != nil {
		logger.WarnCF("attachments", "Failed to parse attachment", map[string]any{
			"name":       name,
			"media_type": mediaType,
			"error":      err.Error(),
		})
		return attachment, buildError(name, "parse_failed", err.Error(),
			summarizeParseError(name, mediaType, err))
	}

	text = normalizeText(text)
	if text == "" {
		return attachment, buildError(name, "empty_content", "",
			fmt.Sprintf("Attachment %q was received but contains no extractable text.", name))
	}

	if utf8.RuneCountInString(text) > p.maxTextChars {
		return attachment, buildError(name, "text_too_large", fmt.Sprintf("%d chars", utf8.RuneCountInString(text)),
			fmt.Sprintf("Attachment %q content is too large for direct understanding. Please split or simplify it.", name))
	}

	attachment.TextContent = text
	return attachment, nil
}

func buildError(name, code, reason, userMessage string) *bus.AttachmentError {
	return &bus.AttachmentError{
		Name:        name,
		Code:        code,
		Reason:      reason,
		UserMessage: userMessage,
	}
}

// summarizeParseError produces a user-facing message that includes the concrete
// failure reason so the LLM can relay it directly instead of guessing.
func summarizeParseError(name, mediaType string, err error) string {
	reason := err.Error()
	return fmt.Sprintf(
		"Attachment %q (%s) was received but could not be parsed: %s. "+
			"The file may use an unsupported encoding or structure.",
		name, mediaType, reason)
}

func detectMediaType(path, ext string) string {
	f, err := os.Open(path)
	if err != nil {
		return mediaTypeFromExt(ext)
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return mediaTypeFromExt(ext)
	}

	contentType := http.DetectContentType(buf[:n])
	if n >= 12 && string(buf[:4]) == "RIFF" && string(buf[8:12]) == "WEBP" {
		contentType = "image/webp"
	}

	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	if contentType == "" || contentType == "application/octet-stream" {
		return mediaTypeFromExt(ext)
	}

	if extType := mediaTypeFromExt(ext); extType != "" {
		if contentType == "text/plain" && (ext == ".pdf" || ext == ".docx" || ext == ".xlsx") {
			return extType
		}
		if contentType == "application/zip" && (ext == ".docx" || ext == ".xlsx") {
			return extType
		}
	}

	return contentType
}

// InferMediaTypeFromName infers MIME type from file name extension only.
// 用于只有 file_name 元信息、无法读取文件内容时的统一推断。
func InferMediaTypeFromName(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	return mediaTypeFromExt(ext)
}

// InferAttachmentKindFromName infers attachment kind from file name extension only.
// 该函数与 Processor 内部分类逻辑保持一致，避免调用方规则漂移。
func InferAttachmentKindFromName(fileName string) bus.AttachmentKind {
	ext := strings.ToLower(filepath.Ext(fileName))
	return classifyKind(mediaTypeFromExt(ext), ext)
}

func mediaTypeFromExt(ext string) string {
	if ext == "" {
		return "application/octet-stream"
	}

	mt := mime.TypeByExtension(ext)
	if idx := strings.Index(mt, ";"); idx > 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	if mt != "" {
		return mt
	}

	switch ext {
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".csv":
		return "text/csv"
	case ".md", ".txt", ".log":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func classifyKind(mediaType, ext string) bus.AttachmentKind {
	if strings.HasPrefix(mediaType, "image/") {
		return bus.AttachmentKindImage
	}
	if strings.HasPrefix(mediaType, "audio/") {
		return bus.AttachmentKindAudio
	}
	if strings.HasPrefix(mediaType, "video/") {
		return bus.AttachmentKindVideo
	}

	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return bus.AttachmentKindImage
	case ".mp3", ".ogg", ".wav", ".m4a", ".amr", ".flac":
		return bus.AttachmentKindAudio
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return bus.AttachmentKindVideo
	}

	if mediaType != "" {
		return bus.AttachmentKindDocument
	}

	return bus.AttachmentKindUnknown
}

type documentType string

const (
	docTypeUnsupported documentType = ""
	docTypePlainText   documentType = "plain_text"
	docTypePDF         documentType = "pdf"
	docTypeDOCX        documentType = "docx"
	docTypeXLSX        documentType = "xlsx"
)

func detectDocumentType(mediaType, ext string) documentType {
	if strings.HasPrefix(mediaType, "text/") {
		return docTypePlainText
	}

	switch ext {
	case ".txt", ".md", ".csv", ".log":
		return docTypePlainText
	case ".pdf":
		return docTypePDF
	case ".docx":
		return docTypeDOCX
	case ".xlsx":
		return docTypeXLSX
	}

	switch mediaType {
	case "application/pdf":
		return docTypePDF
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return docTypeDOCX
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return docTypeXLSX
	}

	return docTypeUnsupported
}

func (p *Processor) extractText(path string, docType documentType) (string, error) {
	switch docType {
	case docTypePlainText:
		return extractPlainText(path)
	case docTypePDF:
		return extractPDFText(path, p.maxTextChars)
	case docTypeDOCX:
		return extractDOCXText(path)
	case docTypeXLSX:
		return extractXLSXText(path)
	default:
		return "", fmt.Errorf("unsupported document type")
	}
}

func extractPlainText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return decodeTextBytes(data), nil
}

// extractPDFText uses github.com/ledongthuc/pdf to extract text.
// It handles CIDFont + ToUnicode CMap encodings commonly used in Chinese PDFs.
func extractPDFText(path string, maxTextChars int) (string, error) {
	if maxTextChars <= 0 {
		maxTextChars = defaultMaxTextChars
	}

	f, reader, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	plainText, err := reader.GetPlainText()
	if err != nil {
		return "", err
	}

	// LimitReader caps memory usage, *4 for UTF-8 worst case per rune
	limited := io.LimitReader(plainText, int64(maxTextChars)*4)
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(limited); err != nil {
		return "", err
	}

	text := buf.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no extractable text found in PDF")
	}
	return text, nil
}

func extractDOCXText(path string) (string, error) {
	document, err := godocx.OpenDocument(path)
	if err != nil {
		return "", err
	}

	if document.Document == nil || document.Document.Body == nil {
		return "", fmt.Errorf("document body not found")
	}

	var out strings.Builder
	for _, child := range document.Document.Body.Children {
		if child.Para == nil {
			continue
		}
		appendParagraphText(&out, child.Para.GetCT().Children)
		appendNewline(&out)
	}

	return out.String(), nil
}

func extractXLSXText(path string) (string, error) {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = workbook.Close()
	}()

	sheetNames := workbook.GetSheetList()

	if len(sheetNames) == 0 {
		return "", fmt.Errorf("worksheets not found")
	}

	var out strings.Builder
	for index, sheet := range sheetNames {
		if index > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString("[sheet: ")
		out.WriteString(sheet)
		out.WriteString("]\n")

		rows, readErr := workbook.GetRows(sheet)
		if readErr != nil {
			return "", readErr
		}

		for rowIndex, row := range rows {
			parts := make([]string, 0, len(row))
			for colIndex, cellValue := range row {
				cellValue = strings.TrimSpace(cellValue)
				if cellValue == "" {
					continue
				}

				label, labelErr := excelize.CoordinatesToCellName(colIndex+1, rowIndex+1)
				if labelErr != nil {
					return "", labelErr
				}
				parts = append(parts, label+"="+cellValue)
			}

			if len(parts) > 0 {
				out.WriteString(strings.Join(parts, "\t"))
				out.WriteByte('\n')
			}
		}
	}

	return out.String(), nil
}

func decodeTextBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	if len(data) >= 2 {
		if data[0] == 0xFE && data[1] == 0xFF {
			return decodeUTF16(data[2:], true)
		}
		if data[0] == 0xFF && data[1] == 0xFE {
			return decodeUTF16(data[2:], false)
		}
	}

	if looksLikeUTF16(data) {
		return decodeUTF16(data, true)
	}

	if utf8.Valid(data) {
		return string(data)
	}

	return string(bytes.ToValidUTF8(data, []byte("�")))
}

func looksLikeUTF16(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	zeroCount := 0
	sample := len(data)
	if sample > 200 {
		sample = 200
	}
	for index := 1; index < sample; index += 2 {
		if data[index] == 0 {
			zeroCount++
		}
	}

	return zeroCount > sample/8
}

func decodeUTF16(data []byte, bigEndian bool) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return ""
	}

	words := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		if bigEndian {
			words = append(words, uint16(data[index])<<8|uint16(data[index+1]))
		} else {
			words = append(words, uint16(data[index+1])<<8|uint16(data[index]))
		}
	}

	return string(utf16.Decode(words))
}

func appendParagraphText(builder *strings.Builder, children []ctypes.ParagraphChild) {
	for _, child := range children {
		if child.Run != nil {
			for _, runChild := range child.Run.Children {
				switch {
				case runChild.Text != nil:
					builder.WriteString(runChild.Text.Text)
				case runChild.DelText != nil:
					builder.WriteString(runChild.DelText.Text)
				case runChild.Tab != nil:
					builder.WriteByte('\t')
				case runChild.Break != nil || runChild.CarrRtn != nil:
					appendNewline(builder)
				}
			}
		}

		if child.Link != nil {
			appendParagraphText(builder, child.Link.Children)
		}
	}
}

func appendNewline(builder *strings.Builder) {
	if builder.Len() == 0 {
		return
	}
	current := builder.String()
	if strings.HasSuffix(current, "\n") {
		return
	}
	builder.WriteByte('\n')
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	out := make([]string, 0, len(lines))
	blankCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blankCount = 0
		out = append(out, trimmed)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}
