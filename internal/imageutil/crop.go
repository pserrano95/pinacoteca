package imageutil

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
)

// Rect uses fractions of the oriented, rotated and straightened canvas.
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Crop struct {
	QuarterTurns  int     `json:"quarter_turns"`
	StraightenDeg float64 `json:"straighten_deg"`
	Rect          Rect    `json:"rect"`
}

func (c Crop) Validate() error {
	r := c.Rect
	for _, v := range []float64{r.X, r.Y, r.W, r.H, c.StraightenDeg} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid crop: non-finite value")
		}
	}
	if c.QuarterTurns < 0 || c.QuarterTurns > 3 || c.StraightenDeg < -15 || c.StraightenDeg > 15 || math.Trunc(c.StraightenDeg*2) != c.StraightenDeg*2 ||
		r.X < 0 || r.Y < 0 || r.X >= 1 || r.Y >= 1 || r.W <= 0 || r.H <= 0 || r.X+r.W > 1 || r.Y+r.H > 1 {
		return fmt.Errorf("invalid crop: out of range")
	}
	return nil
}

// ApplyCrop is pure. src must already have its EXIF orientation applied.
// Positive angles rotate clockwise in image coordinates; uncovered pixels are white.
func ApplyCrop(src image.Image, c Crop) (image.Image, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.QuarterTurns {
	case 1:
		src = ApplyOrientation(src, 6)
	case 2:
		src = ApplyOrientation(src, 3)
	case 3:
		src = ApplyOrientation(src, 8)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if c.StraightenDeg != 0 {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		a := c.StraightenDeg * math.Pi / 180
		co, si := math.Cos(a), math.Sin(a)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dx, dy := float64(x)+0.5-float64(w)/2, float64(y)+0.5-float64(h)/2
				sx, sy := int(math.Floor(co*dx+si*dy+float64(w)/2)), int(math.Floor(-si*dx+co*dy+float64(h)/2))
				if sx >= 0 && sx < w && sy >= 0 && sy < h {
					dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
				} else {
					dst.Set(x, y, color.White)
				}
			}
		}
		src = dst
		b = src.Bounds()
	}
	// Outward rounding preserves even a subpixel selection and never creates an empty image.
	x0, y0 := int(math.Floor(c.Rect.X*float64(w))), int(math.Floor(c.Rect.Y*float64(h)))
	x1, y1 := min(w, int(math.Ceil((c.Rect.X+c.Rect.W)*float64(w)))), min(h, int(math.Ceil((c.Rect.Y+c.Rect.H)*float64(h))))
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min.Add(image.Pt(x0, y0)), draw.Over)
	return dst, nil
}

// WriteCropped derives both sizes independently from the transformed original.
func WriteCropped(originalPath, derivedDir string, c Crop) (int, error) {
	if err := c.Validate(); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(originalPath)
	if err != nil {
		return 0, err
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	orientation := 1
	if format == "jpeg" {
		orientation = ReadOrientation(data)
		src = ApplyOrientation(src, orientation)
	}
	img, err := ApplyCrop(src, c)
	if err != nil {
		return 0, err
	}
	if err = os.MkdirAll(derivedDir, 0755); err != nil {
		return 0, err
	}
	for name, edge := range map[string]int{"display.jpg": 2560, "thumb.jpg": 400} {
		var out bytes.Buffer
		if err = jpeg.Encode(&out, resize(img, edge), &jpeg.Options{Quality: 85}); err != nil {
			return 0, err
		}
		if err = os.WriteFile(filepath.Join(derivedDir, name), out.Bytes(), 0644); err != nil {
			return 0, err
		}
	}
	meta := struct {
		SchemaVersion int    `json:"schema_version"`
		Crop          Crop   `json:"crop"`
		Orientation   int    `json:"orientation"`
		SHA256        string `json:"sha256"`
		MaxEdge       int    `json:"max_edge"`
	}{1, c, orientation, fmt.Sprintf("%x", sha256.Sum256(data)), 2560}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return 0, err
	}
	err = os.WriteFile(filepath.Join(derivedDir, "display.json"), append(raw, '\n'), 0644)
	return orientation, err
}
