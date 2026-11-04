package imageutil

import (
	"bytes"
	"encoding/binary"
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

// WriteThumbnail decodes the original, applies EXIF orientation if it is a JPEG,
// and writes a JPEG thumbnail under derived/. Returns the applied orientation.
func WriteThumbnail(originalPath, thumbPath string, maxEdge int) (int, error) {
	data, err := os.ReadFile(originalPath)
	if err != nil {
		return 0, err
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("decode for thumbnail: %w", err)
	}

	orientation := 1
	if format == "jpeg" {
		orientation = ReadOrientation(data)
		img = ApplyOrientation(img, orientation)
	}

	scaled := resize(img, maxEdge)
	if err := os.MkdirAll(filepath.Dir(thumbPath), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(thumbPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	if err := jpeg.Encode(out, scaled, &jpeg.Options{Quality: 85}); err != nil {
		return 0, err
	}
	return orientation, nil
}

// ReadOrientation inspects the first APP1 Exif segment of a JPEG byte slice
// and returns the EXIF Orientation tag value (1–8). If EXIF is absent, malformed,
// truncated, or the orientation value is outside 1–8, it safely returns 1.
func ReadOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}

	idx := 2
	for idx < len(data) {
		if data[idx] != 0xff {
			return 1
		}
		for idx < len(data) && data[idx] == 0xff {
			idx++
		}
		if idx >= len(data) {
			return 1
		}
		marker := data[idx]
		idx++

		// SOS (0xda) begins entropy data; EOI (0xd9) ends image.
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		// Restart markers (0xd0-0xd7) and TEM (0x01) have no length.
		if (marker >= 0xd0 && marker <= 0xd7) || marker == 0x01 {
			continue
		}

		if idx+2 > len(data) {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(data[idx : idx+2]))
		if segLen < 2 || idx+segLen > len(data) {
			return 1
		}

		payload := data[idx+2 : idx+segLen : idx+segLen]
		idx += segLen

		if marker == 0xe1 {
			if len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				return parseExifOrientation(payload[6:])
			}
		}
	}
	return 1
}

func parseExifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}

	var byteOrder binary.ByteOrder
	if tiff[0] == 'I' && tiff[1] == 'I' {
		byteOrder = binary.LittleEndian
	} else if tiff[0] == 'M' && tiff[1] == 'M' {
		byteOrder = binary.BigEndian
	} else {
		return 1
	}

	if byteOrder.Uint16(tiff[2:4]) != 42 {
		return 1
	}

	ifdOffset := int(byteOrder.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return 1
	}

	numEntries := int(byteOrder.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2
	for i := 0; i < numEntries; i++ {
		entryOffset := entriesStart + i*12
		if entryOffset+12 > len(tiff) {
			return 1
		}
		tag := byteOrder.Uint16(tiff[entryOffset : entryOffset+2])
		if tag == 0x0112 {
			typ := byteOrder.Uint16(tiff[entryOffset+2 : entryOffset+4])
			count := byteOrder.Uint32(tiff[entryOffset+4 : entryOffset+8])
			if count == 0 {
				return 1
			}
			valBytes := tiff[entryOffset+8 : entryOffset+12]
			var val int
			switch typ {
			case 3: // SHORT (uint16)
				val = int(byteOrder.Uint16(valBytes[0:2]))
			case 4: // LONG (uint32)
				val = int(byteOrder.Uint32(valBytes[0:4]))
			case 1: // BYTE (uint8)
				val = int(valBytes[0])
			default:
				return 1
			}
			if val >= 1 && val <= 8 {
				return val
			}
			return 1
		}
	}
	return 1
}

// ApplyOrientation returns a new image rotated and/or mirrored according to the
// EXIF orientation value (1–8). If orientation is 1 or out of range, it returns src.
func ApplyOrientation(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}

	if orientation <= 4 {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := src.At(b.Min.X+x, b.Min.Y+y)
				var dx, dy int
				switch orientation {
				case 2: // Flip horizontal
					dx, dy = w-1-x, y
				case 3: // Rotate 180
					dx, dy = w-1-x, h-1-y
				case 4: // Flip vertical
					dx, dy = x, h-1-y
				}
				dst.Set(dx, dy, c)
			}
		}
		return dst
	}

	// Orientations 5..8 swap dimensions (width becomes h, height becomes w).
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			var dx, dy int
			switch orientation {
			case 5: // Transpose
				dx, dy = y, x
			case 6: // Rotate 90 CW
				dx, dy = h-1-y, x
			case 7: // Transverse
				dx, dy = h-1-y, w-1-x
			case 8: // Rotate 270 CW (90 CCW)
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, c)
		}
	}
	return dst
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
