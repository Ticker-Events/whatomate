package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/pkg/whatsapp"
	"golang.org/x/image/webp"
)

// WhatsApp image messages and interactive image headers reject WebP.
const maxWhatsAppImageBytes int64 = 5 << 20 // 5 MiB
const jpegEncodeQuality = 85

func isWebPPathOrMIME(mimeType, nameOrURL string) bool {
	if strings.EqualFold(strings.TrimSpace(mimeType), "image/webp") {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(nameOrURL))
	if lower == "" {
		return false
	}
	if parsed, err := url.Parse(lower); err == nil && parsed.Path != "" {
		lower = parsed.Path
	} else if i := strings.IndexAny(lower, "?#"); i >= 0 {
		lower = lower[:i]
	}
	return strings.HasSuffix(lower, ".webp")
}

func isWebPBytes(data []byte) bool {
	return len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WEBP"
}

func looksLikeWebP(mimeType, nameOrURL string, data []byte) bool {
	return isWebPPathOrMIME(mimeType, nameOrURL) || isWebPBytes(data)
}

func webpToJPEG(data []byte) ([]byte, error) {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode webp: %w", err)
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: jpegEncodeQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// ensureJPEGForWhatsApp converts WebP bytes to JPEG when needed. Non-WebP
// inputs are returned unchanged.
func ensureJPEGForWhatsApp(data []byte, mimeType, filename string) (out []byte, outMIME, outName string, err error) {
	if !looksLikeWebP(mimeType, filename, data) {
		return data, mimeType, filename, nil
	}
	jpegData, err := webpToJPEG(data)
	if err != nil {
		return nil, "", "", err
	}
	name := strings.TrimSpace(filename)
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if base == "" || base == "." || base == "/" {
		base = "image"
	}
	return jpegData, "image/jpeg", base + ".jpg", nil
}

// downloadPublicMedia fetches a public HTTP(S) image with SSRF protections.
func downloadPublicMedia(ctx context.Context, mediaURL string, maxBytes int64) ([]byte, error) {
	if _, err := validatePublicMediaURL(ctx, mediaURL); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = maxWhatsAppImageBytes
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolvePublicMediaHost(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many media redirects")
			}
			_, err := validatePublicMediaURL(req.Context(), req.URL.String())
			return err
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media URL returned status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("media exceeds %d bytes", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("media exceeds %d bytes", maxBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty media body")
	}
	return data, nil
}

// resolveWhatsAppImageRef returns a WhatsApp-safe image reference for header
// and carousel cards. JPEG/PNG HTTPS links pass through. WebP URLs are
// downloaded, transcoded to JPEG, and uploaded via resolveOutgoingMediaRef
// (Meta media ID or AiSensy HTTPS URL). cache dedupes repeated URLs.
func (a *App) resolveWhatsAppImageRef(ctx context.Context, account *models.WhatsAppAccount, mediaURL string, cache map[string]string) (string, error) {
	mediaURL = strings.TrimSpace(mediaURL)
	if mediaURL == "" {
		return "", nil
	}
	if !isWebPPathOrMIME("", mediaURL) {
		return mediaURL, nil
	}
	if cache != nil {
		if hit, ok := cache[mediaURL]; ok {
			return hit, nil
		}
	}
	data, err := downloadPublicMedia(ctx, mediaURL, maxWhatsAppImageBytes)
	if err != nil {
		return "", fmt.Errorf("download webp: %w", err)
	}
	jpegData, mime, name, err := ensureJPEGForWhatsApp(data, "image/webp", path.Base(mediaURL))
	if err != nil {
		return "", err
	}
	ref, err := a.resolveOutgoingMediaRef(ctx, account, jpegData, mime, name)
	if err != nil {
		return "", fmt.Errorf("upload converted jpeg: %w", err)
	}
	if cache != nil {
		cache[mediaURL] = ref
	}
	return ref, nil
}

// prepareInteractiveHeaderImage converts a WebP header URL to a WhatsApp-safe
// ref. On failure the header is omitted so the button message still sends.
func (a *App) prepareInteractiveHeaderImage(ctx context.Context, account *models.WhatsAppAccount, headerURL string) string {
	headerURL = strings.TrimSpace(headerURL)
	if headerURL == "" {
		return ""
	}
	ref, err := a.resolveWhatsAppImageRef(ctx, account, headerURL, nil)
	if err != nil {
		a.Log.Warn("Failed to convert header image; sending without header",
			"error", err, "host", hostOfURL(headerURL))
		return ""
	}
	return ref
}

// prepareCarouselImages converts WebP image card URLs. Video cards are left
// unchanged. A conversion failure aborts the send so WhatsApp never receives a WebP link.
func (a *App) prepareCarouselImages(ctx context.Context, account *models.WhatsAppAccount, params whatsapp.CarouselMessageParams) (whatsapp.CarouselMessageParams, error) {
	if len(params.Cards) == 0 {
		return params, nil
	}
	out := whatsapp.CarouselMessageParams{
		Cards: make([]whatsapp.CarouselCard, len(params.Cards)),
	}
	copy(out.Cards, params.Cards)
	cache := make(map[string]string)
	for i := range out.Cards {
		if strings.EqualFold(strings.TrimSpace(out.Cards[i].MediaType), "video") {
			continue
		}
		ref, err := a.resolveWhatsAppImageRef(ctx, account, out.Cards[i].MediaURL, cache)
		if err != nil {
			return whatsapp.CarouselMessageParams{}, fmt.Errorf("convert carousel image: %w", err)
		}
		out.Cards[i].MediaURL = ref
	}
	return out, nil
}
