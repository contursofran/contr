package apptest

import (
	"image"
	stdcolor "image/color"
)

// PixelAt reads the colour of img at (x, y). The Tester image is at scale 1,
// so DIPs equal pixels. Read the image once with tt.Image(): each call
// renders the frame again, which is slow under -race.
func PixelAt(img *image.RGBA, x, y float32) stdcolor.RGBA {
	return img.RGBAAt(int(x), int(y))
}
