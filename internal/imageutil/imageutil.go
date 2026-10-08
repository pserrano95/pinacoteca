package imageutil

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Allowed MIME types for artwork originals (decoded by the standard library).
var allowed = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// DetectExt sniffs the content type and returns a filesystem extension.
func DetectExt(data []byte) (string, error) {
	ct := http.DetectContentType(data)
	if ext, ok := allowed[ct]; ok {
		// Confirm it actually decodes.
		if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
			return "", fmt.Errorf("not a valid image: %w", err)
		}
		return ext, nil
	}
	return "", fmt.Errorf("not an allowed image (detected %s)", ct)
}

// WriteOriginal copies raw bytes unchanged to destPath.
func WriteOriginal(destPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, data, 0o644)
}

// WriteThumbnail decodes the original and writes a JPEG thumbnail under derived/.
func WriteThumbnail(originalPath, thumbPath string, maxEdge int) error {
	f, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode for thumbnail: %w", err)
	}
	scaled := resize(img, maxEdge)
	if err := os.MkdirAll(filepath.Dir(thumbPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(thumbPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return jpeg.Encode(out, scaled, &jpeg.Options{Quality: 85})
}

func resize(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	nw, nh := w, h
	if w >= h && w > maxEdge {
		nw = maxEdge
		nh = h * maxEdge / w
	} else if h > maxEdge {
		nh = maxEdge
		nw = w * maxEdge / h
	}
	if nw == w && nh == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			sy := b.Min.Y + y*h/nh
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// ReadLimited reads r up to maxBytes+1 to detect overflow.
func ReadLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file too large (max %d bytes)", maxBytes)
	}
	return data, nil
}
