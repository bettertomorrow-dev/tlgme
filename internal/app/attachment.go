package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-telegram/bot/models"
)

type attachment struct {
	file        models.InputFile
	filename    string
	contentType string
	size        int
	upload      bool
}

type outgoing struct {
	text       string
	attachment *attachment
	asDocument bool
	fallback   bool
	buttons    []string
}

func (opts cliOptions) outgoing(stdin io.Reader) (outgoing, error) {
	message := outgoing{text: opts.text.value, buttons: opts.buttons}
	source := opts.image
	if opts.file.set {
		source = opts.file
		message.asDocument = true
	}
	if !source.set {
		return message, nil
	}
	if len(message.text) > maxCaptionLength {
		return outgoing{}, fmt.Errorf("--text caption must be at most %d characters", maxCaptionLength)
	}
	attachment, err := resolveAttachmentFrom(source.value, opts.filename.value, stdin)
	if err != nil {
		return outgoing{}, err
	}
	if !message.asDocument && attachment.upload && (attachment.size > maxPhotoSize || !strings.HasPrefix(attachment.contentType, "image/")) {
		message.asDocument = true
		message.fallback = true
	}
	message.attachment = &attachment
	return message, nil
}

func resolveAttachmentFrom(source, filename string, stdin io.Reader) (attachment, error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		if filename == "" {
			filename = filepath.Base(strings.Split(strings.TrimRight(source, "/"), "?")[0])
		}
		return attachment{file: &models.InputFileString{Data: source}, filename: filename}, nil
	}

	var data []byte
	var inferredName string
	switch {
	case strings.HasPrefix(source, "data:"):
		comma := strings.IndexByte(source, ',')
		if comma < 0 || !strings.Contains(source[:comma], ";base64") {
			return attachment{}, errors.New("attachment data URI must be base64 encoded")
		}
		decoded, err := decodeBase64(source[comma+1:])
		if err != nil {
			return attachment{}, fmt.Errorf("decode attachment data URI: %w", err)
		}
		data = decoded
	case source == "-":
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return attachment{}, fmt.Errorf("read attachment stdin: %w", err)
		}
		if decoded, err := decodeBase64(string(raw)); err == nil {
			data = decoded
		} else {
			data = raw
		}
	default:
		path := expandHome(source)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return attachment{}, fmt.Errorf("read attachment: %w", err)
			}
			inferredName = filepath.Base(path)
		} else if decoded, err := decodeBase64(source); err == nil && len(strings.TrimSpace(source)) >= 64 {
			data = decoded
		} else {
			return attachment{}, errors.New("attachment must be a URL, a file path, a data URI, or base64 data")
		}
	}
	contentType := http.DetectContentType(data)
	if filename == "" {
		filename = inferredName
	}
	if filename == "" {
		filename = filenameForContentType(contentType)
	}
	return attachment{
		file:        &models.InputFileUpload{Filename: filename, Data: bytes.NewReader(data)},
		filename:    filename,
		contentType: contentType,
		size:        len(data),
		upload:      true,
	}, nil
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, value)
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(value)
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func filenameForContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return "image.png"
	case "image/jpeg":
		return "image.jpg"
	case "image/gif":
		return "image.gif"
	case "application/pdf":
		return "file.pdf"
	default:
		return "file.bin"
	}
}
