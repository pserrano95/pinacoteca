package imageutil_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pserrano95/pinacoteca/internal/imageutil"
)

var (
	red    = color.RGBA{R: 255, G: 0, B: 0, A: 255}
	green  = color.RGBA{R: 0, G: 255, B: 0, A: 255}
	yellow = color.RGBA{R: 255, G: 255, B: 0, A: 255}
	blue   = color.RGBA{R: 0, G: 0, B: 255, A: 255}
)

// createCornerImage creates a w×h image divided into 4 colored quadrants:
// Top-Left: Red, Top-Right: Green, Bottom-Left: Yellow, Bottom-Right: Blue.
func createCornerImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	halfW := w / 2
	halfH := h / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < halfW && y < halfH {
				img.Set(x, y, red)
			} else if x >= halfW && y < halfH {
				img.Set(x, y, green)
			} else if x < halfW && y >= halfH {
				img.Set(x, y, yellow)
			} else {
				img.Set(x, y, blue)
			}
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func makeExifAPP1(byteOrder binary.ByteOrder, orientation int, ifdOffsetOverride int) []byte {
	var tiff bytes.Buffer
	var orderBytes []byte
	if byteOrder == binary.LittleEndian {
		orderBytes = []byte{'I', 'I'}
	} else {
		orderBytes = []byte{'M', 'M'}
	}
	tiff.Write(orderBytes)
	_ = binary.Write(&tiff, byteOrder, uint16(42))
	ifdOffset := uint32(8)
	if ifdOffsetOverride != 0 {
		ifdOffset = uint32(ifdOffsetOverride)
	}
	_ = binary.Write(&tiff, byteOrder, ifdOffset)

	// IFD0
	_ = binary.Write(&tiff, byteOrder, uint16(1)) // 1 entry
	// Tag 0x0112 (Orientation)
	_ = binary.Write(&tiff, byteOrder, uint16(0x0112))
	_ = binary.Write(&tiff, byteOrder, uint16(3)) // SHORT
	_ = binary.Write(&tiff, byteOrder, uint32(1)) // count = 1
	// Value (2 bytes) + 2 bytes padding
	_ = binary.Write(&tiff, byteOrder, uint16(orientation))
	_ = binary.Write(&tiff, byteOrder, uint16(0))
	// Next IFD offset
	_ = binary.Write(&tiff, byteOrder, uint32(0))

	tiffBytes := tiff.Bytes()
	payload := append([]byte("Exif\x00\x00"), tiffBytes...)

	segLen := uint16(2 + len(payload))
	var seg bytes.Buffer
	seg.WriteByte(0xff)
	seg.WriteByte(0xe1)
	_ = binary.Write(&seg, binary.BigEndian, segLen)
	seg.Write(payload)

	return seg.Bytes()
}

func insertAPP1(jpegBytes, app1Bytes []byte) []byte {
	res := make([]byte, 0, len(jpegBytes)+len(app1Bytes))
	res = append(res, jpegBytes[:2]...)
	res = append(res, app1Bytes...)
	res = append(res, jpegBytes[2:]...)
	return res
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func colorClose(c color.Color, exp color.RGBA, tolerance int) bool {
	r, g, b, _ := c.RGBA()
	gotR := uint8(r >> 8)
	gotG := uint8(g >> 8)
	gotB := uint8(b >> 8)
	return absDiff(gotR, exp.R) <= tolerance &&
		absDiff(gotG, exp.G) <= tolerance &&
		absDiff(gotB, exp.B) <= tolerance
}

func expectedTopLeft(orientation int) color.RGBA {
	switch orientation {
	case 1:
		return red
	case 2:
		return green
	case 3:
		return blue
	case 4:
		return yellow
	case 5:
		return red
	case 6:
		return yellow
	case 7:
		return blue
	case 8:
		return green
	default:
		return red
	}
}

func TestReadOrientation(t *testing.T) {
	t.Run("nil or empty", func(t *testing.T) {
		if got := imageutil.ReadOrientation(nil); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
		if got := imageutil.ReadOrientation([]byte{0xff, 0xd8}); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	})

	t.Run("jpeg without APP1", func(t *testing.T) {
		raw := encodeJPEG(t, createCornerImage(40, 20))
		if got := imageutil.ReadOrientation(raw); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	})

	t.Run("orientations 1 to 8 II and MM", func(t *testing.T) {
		base := encodeJPEG(t, createCornerImage(40, 20))
		orders := []struct {
			name  string
			order binary.ByteOrder
		}{
			{"II", binary.LittleEndian},
			{"MM", binary.BigEndian},
		}

		for _, ord := range orders {
			for orient := 1; orient <= 8; orient++ {
				t.Run(ord.name+"_"+string(rune('0'+orient)), func(t *testing.T) {
					app1 := makeExifAPP1(ord.order, orient, 0)
					jpegWithExif := insertAPP1(base, app1)
					got := imageutil.ReadOrientation(jpegWithExif)
					if got != orient {
						t.Fatalf("expected orientation %d, got %d", orient, got)
					}
				})
			}
		}
	})

	t.Run("truncated APP1 segment", func(t *testing.T) {
		base := encodeJPEG(t, createCornerImage(40, 20))
		app1 := makeExifAPP1(binary.LittleEndian, 6, 0)
		// Truncate inside payload
		truncated := insertAPP1(base, app1[:len(app1)-10])
		if got := imageutil.ReadOrientation(truncated); got != 1 {
			t.Fatalf("expected 1 for truncated APP1, got %d", got)
		}
	})

	t.Run("IFD pointing outside segment", func(t *testing.T) {
		base := encodeJPEG(t, createCornerImage(40, 20))
		app1 := makeExifAPP1(binary.LittleEndian, 6, 9999)
		withExif := insertAPP1(base, app1)
		if got := imageutil.ReadOrientation(withExif); got != 1 {
			t.Fatalf("expected 1 for out of bounds IFD, got %d", got)
		}
	})

	t.Run("orientation out of 1-8 range", func(t *testing.T) {
		base := encodeJPEG(t, createCornerImage(40, 20))
		app1 := makeExifAPP1(binary.LittleEndian, 9, 0)
		withExif := insertAPP1(base, app1)
		if got := imageutil.ReadOrientation(withExif); got != 1 {
			t.Fatalf("expected 1 for orientation 9, got %d", got)
		}
	})
}

func TestWriteThumbnail_Orientations(t *testing.T) {
	base := encodeJPEG(t, createCornerImage(40, 20))
	orders := []struct {
		name  string
		order binary.ByteOrder
	}{
		{"II", binary.LittleEndian},
		{"MM", binary.BigEndian},
	}

	for _, ord := range orders {
		for orient := 1; orient <= 8; orient++ {
			t.Run(ord.name+"_"+string(rune('0'+orient)), func(t *testing.T) {
				app1 := makeExifAPP1(ord.order, orient, 0)
				inputBytes := insertAPP1(base, app1)

				dir := t.TempDir()
				origPath := filepath.Join(dir, "original.jpg")
				thumbPath := filepath.Join(dir, "thumb.jpg")

				if err := os.WriteFile(origPath, inputBytes, 0o644); err != nil {
					t.Fatal(err)
				}

				gotOrient, err := imageutil.WriteThumbnail(origPath, thumbPath, 400)
				if err != nil {
					t.Fatalf("WriteThumbnail failed: %v", err)
				}
				if gotOrient != orient {
					t.Fatalf("expected returned orientation %d, got %d", orient, gotOrient)
				}

				thumbFile, err := os.Open(thumbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer thumbFile.Close()

				thumbImg, err := jpeg.Decode(thumbFile)
				if err != nil {
					t.Fatalf("decode thumb: %v", err)
				}

				bounds := thumbImg.Bounds()
				var expW, expH int
				if orient <= 4 {
					expW, expH = 40, 20
				} else {
					expW, expH = 20, 40
				}
				if bounds.Dx() != expW || bounds.Dy() != expH {
					t.Fatalf("orientation %d: expected size %dx%d, got %dx%d",
						orient, expW, expH, bounds.Dx(), bounds.Dy())
				}

				// Verify top-left corner color
				expColor := expectedTopLeft(orient)
				topLeftColor := thumbImg.At(0, 0)
				if !colorClose(topLeftColor, expColor, 35) {
					r, g, b, _ := topLeftColor.RGBA()
					t.Fatalf("orientation %d: expected top-left color ~%+v, got RGB(%d, %d, %d)",
						orient, expColor, uint8(r>>8), uint8(g>>8), uint8(b>>8))
				}
			})
		}
	}
}

func TestWriteThumbnail_MalformedAndNoAPP1(t *testing.T) {
	base := encodeJPEG(t, createCornerImage(40, 20))

	cases := []struct {
		name string
		data []byte
	}{
		{
			name: "without APP1",
			data: base,
		},
		{
			name: "truncated APP1",
			data: insertAPP1(base, func() []byte {
				// APP1 with valid JPEG segment length but truncated Exif TIFF payload
				payload := []byte("Exif\x00\x00II\x2a\x00")
				segLen := uint16(2 + len(payload))
				var seg bytes.Buffer
				seg.WriteByte(0xff)
				seg.WriteByte(0xe1)
				_ = binary.Write(&seg, binary.BigEndian, segLen)
				seg.Write(payload)
				return seg.Bytes()
			}()),
		},
		{
			name: "IFD out of bounds",
			data: insertAPP1(base, makeExifAPP1(binary.LittleEndian, 6, 9999)),
		},
		{
			name: "orientation 9",
			data: insertAPP1(base, makeExifAPP1(binary.LittleEndian, 9, 0)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			origPath := filepath.Join(dir, "original.jpg")
			thumbPath := filepath.Join(dir, "thumb.jpg")

			if err := os.WriteFile(origPath, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}

			orient, err := imageutil.WriteThumbnail(origPath, thumbPath, 400)
			if err != nil {
				t.Fatalf("WriteThumbnail: %v", err)
			}
			if orient != 1 {
				t.Fatalf("expected orientation 1, got %d", orient)
			}

			thumbFile, err := os.Open(thumbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer thumbFile.Close()

			thumbImg, err := jpeg.Decode(thumbFile)
			if err != nil {
				t.Fatalf("decode thumb: %v", err)
			}

			bounds := thumbImg.Bounds()
			if bounds.Dx() != 40 || bounds.Dy() != 20 {
				t.Fatalf("expected 40x20, got %dx%d", bounds.Dx(), bounds.Dy())
			}

			// Top-left is Red for orientation 1
			topLeftColor := thumbImg.At(0, 0)
			if !colorClose(topLeftColor, red, 35) {
				r, g, b, _ := topLeftColor.RGBA()
				t.Fatalf("expected top-left color ~%+v, got RGB(%d, %d, %d)",
					red, uint8(r>>8), uint8(g>>8), uint8(b>>8))
			}
		})
	}
}

func TestWriteThumbnail_PNG(t *testing.T) {
	// PNG should not pass through EXIF reader and thumbnail dimensions/pixels must remain unchanged.
	img := createCornerImage(40, 20)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	pngData := buf.Bytes()

	dir := t.TempDir()
	origPath := filepath.Join(dir, "original.png")
	thumbPath := filepath.Join(dir, "thumb.jpg")

	if err := os.WriteFile(origPath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	orient, err := imageutil.WriteThumbnail(origPath, thumbPath, 400)
	if err != nil {
		t.Fatalf("WriteThumbnail on PNG: %v", err)
	}
	if orient != 1 {
		t.Fatalf("expected orientation 1 for PNG, got %d", orient)
	}

	thumbFile, err := os.Open(thumbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer thumbFile.Close()

	thumbImg, err := jpeg.Decode(thumbFile)
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	bounds := thumbImg.Bounds()
	if bounds.Dx() != 40 || bounds.Dy() != 20 {
		t.Fatalf("expected 40x20, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	if !colorClose(thumbImg.At(0, 0), red, 35) {
		t.Fatalf("expected top-left red for PNG thumbnail")
	}
}
