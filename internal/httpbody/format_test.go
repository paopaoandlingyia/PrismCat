package httpbody

import (
	"bytes"
	"compress/gzip"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestFormatForDisplayDecodesZstdJSON(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("new zstd writer: %v", err)
	}
	body := encoder.EncodeAll([]byte(`{"data":[{"b64_json":"abc123"}]}`), nil)

	formatted := FormatForDisplay("application/json", "zstd", body, FormatOptions{
		MaxOutputBytes: 1024,
	})

	if formatted.Binary {
		t.Fatalf("Binary = true, want decoded JSON text")
	}
	if !formatted.Decoded || formatted.DecodedFrom != "zstd" {
		t.Fatalf("Decoded = %v, DecodedFrom = %q; want zstd", formatted.Decoded, formatted.DecodedFrom)
	}
	if !strings.Contains(formatted.Text, "b64_json") {
		t.Fatalf("decoded text does not contain b64_json")
	}
}

func TestFormatPreviewForDisplayTruncatesPlainText(t *testing.T) {
	formatted := FormatPreviewForDisplay("application/json", "", []byte(`{"message":"hello world"}`), FormatOptions{
		MaxOutputBytes: 10,
	})

	if formatted.Text != `{"message"` {
		t.Fatalf("Text = %q, want truncated preview", formatted.Text)
	}
	if !formatted.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
}

func TestFormatPreviewForDisplayLimitsDecodedGzip(t *testing.T) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write([]byte(strings.Repeat("a", 1024))); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	formatted := FormatPreviewForDisplay("text/plain", "gzip", compressed.Bytes(), FormatOptions{
		MaxOutputBytes: 32,
	})

	if len(formatted.Text) != 32 {
		t.Fatalf("preview length = %d, want 32", len(formatted.Text))
	}
	if !formatted.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if !formatted.Decoded || formatted.DecodedFrom != "gzip" {
		t.Fatalf("Decoded = %v, DecodedFrom = %q; want gzip", formatted.Decoded, formatted.DecodedFrom)
	}
}

func TestFormatForDisplayCanRequireContentEncodingDecode(t *testing.T) {
	formatted := FormatForDisplay("application/json", "gzip", []byte(`{"already":"display"}`), FormatOptions{
		MaxOutputBytes:               1024,
		RequireContentEncodingDecode: true,
	})

	if !formatted.DecodeFailed {
		t.Fatalf("DecodeFailed = false, want true")
	}
	if formatted.Text == `{"already":"display"}` {
		t.Fatalf("Text fell back to display body, want strict decode failure")
	}
}

func TestFormatForDisplaySummarizesMultipartImageRequest(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "gpt-image-2"); err != nil {
		t.Fatalf("write model field: %v", err)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image[]"; filename="000004.jpg"`)
	header.Set("Content-Type", "image/jpg")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0xff, 0xd9}
	if _, err := part.Write(jpeg); err != nil {
		t.Fatalf("write image part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	formatted := FormatForDisplay(writer.FormDataContentType(), "", body.Bytes(), FormatOptions{
		MaxOutputBytes:  1024,
		TrimLargeBase64: false,
	})

	if formatted.Binary {
		t.Fatalf("Binary = true, want multipart JSON summary")
	}
	for _, want := range []string{
		`"content_type": "multipart/form-data"`,
		`"name": "model"`,
		`"value": "gpt-image-2"`,
		`"filename": "000004.jpg"`,
		`"data_url": "data:image/jpg;base64,`,
	} {
		if !strings.Contains(formatted.Text, want) {
			t.Fatalf("formatted multipart does not contain %q:\n%s", want, formatted.Text)
		}
	}
}
