package httpbody

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

var b64Regex = regexp.MustCompile(`(data:[^\s]+?;base64,)?([A-Za-z0-9+/_-]{200,}[=]{0,2})`)

type FormatOptions struct {
	MaxOutputBytes               int64
	TrimLargeBase64              bool
	RequireContentEncodingDecode bool
}

type FormatResult struct {
	Text         string
	Truncated    bool
	Decoded      bool
	DecodedFrom  string
	DecodeFailed bool
	Binary       bool
}

func FormatForDisplay(contentType, contentEncoding string, body []byte, opts FormatOptions) FormatResult {
	if len(body) == 0 {
		return FormatResult{}
	}

	data := body
	decompressed := false
	truncated := false
	decodedFrom := ""

	tokens := normalizedEncodings(contentEncoding)
	if decoded, wasTruncated, appliedEncoding, ok := decodeContentTokens(tokens, body, opts.MaxOutputBytes); ok {
		data = decoded
		decompressed = true
		truncated = truncated || wasTruncated
		decodedFrom = appliedEncoding
	} else if opts.RequireContentEncodingDecode && len(tokens) > 0 {
		return FormatResult{
			Text:         fmt.Sprintf("[encoded content omitted; unable to decode %s]", strings.Join(tokens, ", ")),
			DecodeFailed: true,
		}
	}

	if multipartText, ok := formatMultipartForDisplay(contentType, data, opts.TrimLargeBase64); ok {
		return FormatResult{
			Text:        multipartText,
			Truncated:   truncated,
			Decoded:     decompressed,
			DecodedFrom: decodedFrom,
		}
	}

	if isProbablyText(contentType) && utf8.Valid(data) {
		return FormatResult{
			Text:        sanitizeText(string(data), opts.TrimLargeBase64),
			Truncated:   truncated,
			Decoded:     decompressed,
			DecodedFrom: decodedFrom,
		}
	}

	if utf8.Valid(data) {
		return FormatResult{
			Text:        sanitizeText(string(data), opts.TrimLargeBase64),
			Truncated:   truncated,
			Decoded:     decompressed,
			DecodedFrom: decodedFrom,
		}
	}

	if decompressed {
		if truncated {
			return FormatResult{
				Text:        fmt.Sprintf("[binary content omitted; %d bytes after decompression (truncated)]", len(data)),
				Truncated:   true,
				Decoded:     true,
				DecodedFrom: decodedFrom,
				Binary:      true,
			}
		}
		return FormatResult{
			Text:        fmt.Sprintf("[binary content omitted; %d bytes after decompression]", len(data)),
			Decoded:     true,
			DecodedFrom: decodedFrom,
			Binary:      true,
		}
	}

	return FormatResult{
		Text:   fmt.Sprintf("[binary content omitted; %d bytes captured]", len(body)),
		Binary: true,
	}
}

func FormatPreviewForDisplay(contentType, contentEncoding string, body []byte, opts FormatOptions) FormatResult {
	if len(body) == 0 || opts.MaxOutputBytes <= 0 {
		return FormatResult{}
	}

	previewBody := body
	inputTruncated := false
	if len(normalizedEncodings(contentEncoding)) == 0 && int64(len(body)) > opts.MaxOutputBytes {
		previewBody = truncateBytesForPreview(body, opts.MaxOutputBytes)
		inputTruncated = true
	}

	result := FormatForDisplay(contentType, contentEncoding, previewBody, opts)
	if int64(len(result.Text)) > opts.MaxOutputBytes {
		result.Text = truncateStringForPreview(result.Text, opts.MaxOutputBytes)
		result.Truncated = true
	}
	result.Truncated = result.Truncated || inputTruncated
	return result
}

func formatMultipartForDisplay(contentType string, body []byte, trimLargeBase64 bool) (string, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return "", false
	}

	boundary := params["boundary"]
	if boundary == "" {
		return "", false
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	parts := make([]map[string]any, 0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false
		}

		partBody, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return "", false
		}

		partInfo := map[string]any{
			"name": part.FormName(),
			"size": len(partBody),
		}
		if filename := part.FileName(); filename != "" {
			contentType := part.Header.Get("Content-Type")
			if contentType == "" {
				contentType = http.DetectContentType(partBody)
			}
			sum := sha256.Sum256(partBody)
			partInfo["type"] = "file"
			partInfo["filename"] = filename
			partInfo["content_type"] = contentType
			partInfo["sha256"] = hex.EncodeToString(sum[:])
			if isImageContentType(contentType) && !trimLargeBase64 {
				partInfo["data_url"] = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(partBody)
			}
		} else {
			partInfo["type"] = "field"
			if utf8.Valid(partBody) {
				partInfo["value"] = sanitizeText(string(partBody), trimLargeBase64)
			} else {
				sum := sha256.Sum256(partBody)
				partInfo["type"] = "binary_field"
				partInfo["content_type"] = http.DetectContentType(partBody)
				partInfo["sha256"] = hex.EncodeToString(sum[:])
			}
		}
		parts = append(parts, partInfo)
	}

	payload := map[string]any{
		"content_type": mediaType,
		"parts":        parts,
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", false
	}
	return string(out), true
}

func isImageContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	return strings.HasPrefix(mediaType, "image/")
}

func decodeContent(contentEncoding string, body []byte, maxOutputBytes int64) ([]byte, bool, string, bool) {
	return decodeContentTokens(normalizedEncodings(contentEncoding), body, maxOutputBytes)
}

func decodeContentTokens(tokens []string, body []byte, maxOutputBytes int64) ([]byte, bool, string, bool) {
	if len(tokens) == 0 {
		return nil, false, "", false
	}

	data := body
	truncated := false

	for i := len(tokens) - 1; i >= 0; i-- {
		decoded, wasTruncated, ok := decodeOnce(tokens[i], data, maxOutputBytes)
		if !ok {
			return nil, false, "", false
		}
		data = decoded
		truncated = truncated || wasTruncated
	}

	return data, truncated, strings.Join(tokens, ", "), true
}

func decodeOnce(encoding string, body []byte, maxOutputBytes int64) ([]byte, bool, bool) {
	switch encoding {
	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, false
		}
		defer r.Close()

		data, truncated, err := readAllLimited(r, maxOutputBytes)
		if err != nil {
			return nil, false, false
		}
		return data, truncated, true
	case "deflate":
		r := flate.NewReader(bytes.NewReader(body))
		defer r.Close()

		data, truncated, err := readAllLimited(r, maxOutputBytes)
		if err != nil {
			return nil, false, false
		}
		return data, truncated, true
	case "br":
		data, truncated, err := readAllLimited(brotli.NewReader(bytes.NewReader(body)), maxOutputBytes)
		if err != nil {
			return nil, false, false
		}
		return data, truncated, true
	case "zstd":
		r, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, false
		}
		defer r.Close()

		data, truncated, err := readAllLimited(r, maxOutputBytes)
		if err != nil {
			return nil, false, false
		}
		return data, truncated, true
	default:
		return nil, false, false
	}
}

func normalizedEncodings(contentEncoding string) []string {
	if contentEncoding == "" {
		return nil
	}

	parts := strings.Split(contentEncoding, ",")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.ToLower(strings.TrimSpace(part))
		if token != "" && token != "identity" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func readAllLimited(r io.Reader, max int64) ([]byte, bool, error) {
	if max <= 0 {
		return nil, false, nil
	}

	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) <= max {
		return data, false, nil
	}
	return data[:max], true, nil
}

func truncateBytesForPreview(body []byte, max int64) []byte {
	if max <= 0 {
		return nil
	}
	if int64(len(body)) <= max {
		return body
	}
	cut := int(max)
	for cut > 0 && (body[cut]&0xC0) == 0x80 {
		cut--
	}
	for cut > 0 && !utf8.Valid(body[:cut]) {
		cut--
		for cut > 0 && (body[cut]&0xC0) == 0x80 {
			cut--
		}
	}
	if cut <= 0 {
		return body[:int(max)]
	}
	return body[:cut]
}

func truncateStringForPreview(text string, max int64) string {
	if max <= 0 {
		return ""
	}
	if int64(len(text)) <= max {
		return text
	}
	cut := int(max)
	for cut > 0 && (text[cut]&0xC0) == 0x80 {
		cut--
	}
	for cut > 0 && !utf8.ValidString(text[:cut]) {
		cut--
		for cut > 0 && (text[cut]&0xC0) == 0x80 {
			cut--
		}
	}
	if cut <= 0 {
		return text[:int(max)]
	}
	return strings.Clone(text[:cut])
}

func sanitizeText(text string, trimLargeBase64 bool) string {
	if !trimLargeBase64 {
		return text
	}

	return b64Regex.ReplaceAllStringFunc(text, func(match string) string {
		if len(match) <= 200 {
			return match
		}
		return match[:200]
	})
}

func isProbablyText(contentType string) bool {
	if contentType == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if mediaType == "application/json" ||
		mediaType == "application/xml" ||
		mediaType == "application/x-www-form-urlencoded" {
		return true
	}
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	return false
}
