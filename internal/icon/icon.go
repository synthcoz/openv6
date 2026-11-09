// Package icon draws the openv6 app and tray icons at runtime (no image
// assets to ship): a rounded square in the state colour with the white
// openv6 mark (hexagon ring + dot), anti-aliased by supersampling.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

type State int

const (
	StateOK     State = iota // all proxies fine
	StateWarn                // some proxies failing
	StateError               // service down / no IPv6
	StatePaused              // proxies paused
	StateBrand               // app icon
)

var colors = map[State]color.RGBA{
	StateOK:     {22, 163, 74, 255},
	StateWarn:   {217, 119, 6, 255},
	StateError:  {220, 38, 38, 255},
	StatePaused: {100, 116, 139, 255},
	StateBrand:  {23, 23, 23, 255},
}

// PNG renders the icon at size×size pixels.
func PNG(s State, size int) []byte {
	const ss = 4 // supersampling factor
	big := size * ss
	bg := colors[s]
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fb := float64(big)
	radius := fb * 0.24
	cx, cy := fb/2, fb/2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px, py := float64(x*ss+sx)+0.5, float64(y*ss+sy)+0.5
					if !inRoundRect(px, py, fb, radius) {
						continue
					}
					// openv6 mark: a hexagon ring (pointy top) around a dot
					// hexagon ring drawn like an SVG stroke with round joins
					sd := sdHexPointy(px-cx, py-cy, fb*0.27)
					d := math.Hypot(px-cx, py-cy)
					white := math.Abs(sd) <= fb*0.036 || d <= fb*0.085
					if white {
						r, g, b = r+255, g+255, b+255
					} else {
						r, g, b = r+float64(bg.R), g+float64(bg.G), b+float64(bg.B)
					}
					a += 255
				}
			}
			n := float64(ss * ss)
			if a == 0 {
				continue
			}
			cov := a / 255
			img.SetNRGBA(x, y, color.NRGBA{uint8(r / cov), uint8(g / cov), uint8(b / cov), uint8(a / n)})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func inRoundRect(x, y, size, r float64) bool {
	m := size * 0.04 // margin
	lo, hi := m, size-m
	if x < lo || x > hi || y < lo || y > hi {
		return false
	}
	cx := math.Max(lo+r, math.Min(x, hi-r))
	cy := math.Max(lo+r, math.Min(y, hi-r))
	return math.Hypot(x-cx, y-cy) <= r
}

// ICO wraps PNG images (16..256 px) into a Windows .ico file.
func ICO(s State, sizes ...int) []byte {
	if len(sizes) == 0 {
		sizes = []int{16, 32, 48, 256}
	}
	var imgs [][]byte
	for _, sz := range sizes {
		imgs = append(imgs, PNG(s, sz))
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for i, sz := range sizes {
		w := byte(sz)
		if sz >= 256 {
			w = 0
		}
		buf.Write([]byte{w, w, 0, 0})
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(imgs[i])))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(imgs[i])
	}
	for _, b := range imgs {
		buf.Write(b)
	}
	return buf.Bytes()
}

// sdHexPointy is the signed distance to a pointy-top regular hexagon with
// apothem a (negative inside).
func sdHexPointy(x, y, a float64) float64 {
	// rotate 90°: the classic formula is for a hexagon with flat top/bottom
	px, py := math.Abs(y), math.Abs(x)
	const kx, ky, kz = -0.866025404, 0.5, 0.577350269
	dot := kx*px + ky*py
	if dot < 0 {
		px -= 2 * dot * kx
		py -= 2 * dot * ky
	}
	cl := math.Max(-kz*a, math.Min(px, kz*a))
	px -= cl
	py -= a
	l := math.Hypot(px, py)
	if py < 0 {
		return -l
	}
	return l
}
