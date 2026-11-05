package imageutil_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/pserrano95/pinacoteca/internal/imageutil"
)

func TestCropQuarterTurns(t *testing.T) {
	for turn, want := range []color.RGBA{red, yellow, blue, green} {
		t.Run(string(rune('0'+turn)), func(t *testing.T) {
			c := imageutil.Crop{QuarterTurns: turn, Rect: imageutil.Rect{W: .5, H: .5}}
			img, err := imageutil.ApplyCrop(createCornerImage(160, 120), c)
			if err != nil {
				t.Fatal(err)
			}
			got := color.RGBAModel.Convert(img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2)).(color.RGBA)
			if got != want {
				t.Fatalf("top-left after turn %d: got %v want %v", turn, got, want)
			}
		})
	}
}

func TestCropStraightenMeasuredPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			src.Set(x, y, color.White)
			if x > 40 && x < 360 && math.Abs(float64(y)-200+math.Tan(10*math.Pi/180)*(float64(x)-200)) < 2 {
				src.Set(x, y, color.Black)
			}
		}
	}
	img, err := imageutil.ApplyCrop(src, imageutil.Crop{StraightenDeg: 10, Rect: imageutil.Rect{W: 1, H: 1}})
	if err != nil {
		t.Fatal(err)
	}
	mean := func(x int) float64 {
		sum, n := 0, 0
		for y := 150; y < 250; y++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r < 1000 {
				sum += y
				n++
			}
		}
		if n == 0 {
			t.Fatal("line missing")
		}
		return float64(sum) / float64(n)
	}
	if delta := math.Abs(mean(80) - mean(320)); delta > 1 {
		t.Fatalf("line not horizontal: vertical drift %.2f pixels (limit 1)", delta)
	}
}

func TestCropJPEGExif6AndDerivatives(t *testing.T) {
	for _, tc := range []struct{ orientation, turn int }{{1, 0}, {1, 1}, {1, 2}, {1, 3}, {6, 0}, {6, 1}} {
		orientation, turn := tc.orientation, tc.turn
		t.Run(fmt.Sprintf("exif%d/turn%d", orientation, turn), func(t *testing.T) {
			data := encodeJPEG(t, createCornerImage(160, 120))
			if orientation == 6 {
				data = append(append(append([]byte{}, data[:2]...), makeExifAPP1(binary.LittleEndian, 6, 0)...), data[2:]...)
			}
			dir := t.TempDir()
			original := filepath.Join(dir, "original.jpg")
			if err := os.WriteFile(original, data, 0644); err != nil {
				t.Fatal(err)
			}
			applied, err := imageutil.WriteCropped(original, filepath.Join(dir, "derived"), imageutil.Crop{QuarterTurns: turn, Rect: imageutil.Rect{W: .5, H: .5}})
			if err != nil {
				t.Fatal(err)
			}
			if applied != orientation {
				t.Fatal(applied)
			}
			effectiveTurn := turn
			if orientation == 6 {
				effectiveTurn = (turn + 1) % 4
			}
			want := []color.RGBA{red, yellow, blue, green}[effectiveTurn]
			for _, name := range []string{"display.jpg", "thumb.jpg"} {
				raw, err := os.ReadFile(filepath.Join(dir, "derived", name))
				if err != nil {
					t.Fatal(err)
				}
				img, err := jpeg.Decode(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				// Every interior pixel must be within 35/255 of the expected quadrant.
				for y := 8; y < img.Bounds().Dy()-8; y++ {
					for x := 8; x < img.Bounds().Dx()-8; x++ {
						got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
						if abs(int(got.R)-int(want.R)) > 35 || abs(int(got.G)-int(want.G)) > 35 || abs(int(got.B)-int(want.B)) > 35 {
							t.Fatalf("%s quadrant: %v want %v", name, got, want)
						}
					}
				}
			}
			after, _ := os.ReadFile(original)
			if !bytes.Equal(data, after) {
				t.Fatal("original bytes changed")
			}
		})
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestCropValidation(t *testing.T) {
	good := imageutil.Crop{Rect: imageutil.Rect{W: 1, H: 1}}
	for name, change := range map[string]func(*imageutil.Crop){
		"x":      func(c *imageutil.Crop) { c.Rect.X = -.1 },
		"y":      func(c *imageutil.Crop) { c.Rect.Y = 1.1 },
		"width":  func(c *imageutil.Crop) { c.Rect.W = 0 },
		"height": func(c *imageutil.Crop) { c.Rect.H = 0 },
		"right":  func(c *imageutil.Crop) { c.Rect.X = .1 },
		"bottom": func(c *imageutil.Crop) { c.Rect.Y = .1 },
		"turn":   func(c *imageutil.Crop) { c.QuarterTurns = 4 },
		"angle":  func(c *imageutil.Crop) { c.StraightenDeg = 20 },
		"step":   func(c *imageutil.Crop) { c.StraightenDeg = .1 },
		"nan":    func(c *imageutil.Crop) { c.Rect.W = math.NaN() },
		"inf":    func(c *imageutil.Crop) { c.Rect.Y = math.Inf(1) },
	} {
		t.Run(name, func(t *testing.T) {
			c := good
			change(&c)
			if _, err := imageutil.ApplyCrop(createCornerImage(8, 8), c); err == nil {
				t.Fatal("invalid crop accepted")
			}
		})
	}
}

func TestCropDisplayBoundAndSubpixel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "original.jpg")
	if err := os.WriteFile(path, encodeJPEG(t, createCornerImage(3000, 20)), 0644); err != nil {
		t.Fatal(err)
	}
	for _, r := range []imageutil.Rect{{W: 1, H: 1}, {X: .99, W: .00001, H: 1}} {
		if _, err := imageutil.WriteCropped(path, dir, imageutil.Crop{Rect: r}); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "display.jpg"))
		config, err := jpeg.DecodeConfig(bytes.NewReader(raw))
		if err != nil || config.Width > 2560 || config.Height > 2560 || config.Width < 1 {
			t.Fatalf("bad dimensions %+v %v", config, err)
		}
	}
}
