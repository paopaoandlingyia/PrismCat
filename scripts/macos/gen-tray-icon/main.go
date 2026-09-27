// Command gen-tray-icon 生成 macOS 菜单栏用的模板图标。
//
// 模板图标只用黑色 + 透明度表达形状，由系统按深色/浅色菜单栏自动着色，
// 所以不能直接复用 Windows 托盘那张彩色图标。图形取自 web/public/favicon.svg
// 的外轮廓和内部切割线。
//
//	go run ./scripts/macos/gen-tray-icon                  # 写入 cmd/prismcat/tray_icon_darwin.png
//	go run ./scripts/macos/gen-tray-icon -out /tmp/x.png
package main

import (
	"flag"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

// systray 以 16pt 显示菜单栏图标，Retina 下对应 32px。
const size = 32

// 每个像素 subsamples×subsamples 次采样做抗锯齿。
const subsamples = 8

type point struct{ x, y float64 }

type stroke struct {
	points []point
	closed bool
	width  float64 // 像素
}

func main() {
	out := flag.String("out", "cmd/prismcat/tray_icon_darwin.png", "输出 PNG 路径")
	flag.Parse()

	// favicon.svg 的 viewBox 是 (14,14)-(86,92)，等比缩放到画布内并居中。
	const scale = 29.0 / 78.0
	offX := (size - 72*scale) / 2
	offY := (size - 78*scale) / 2
	p := func(x, y float64) point { return point{offX + (x-14)*scale, offY + (y-14)*scale} }

	strokes := []stroke{
		{ // 外轮廓
			points: []point{p(32, 18), p(44, 32), p(56, 32), p(68, 18), p(82, 40), p(82, 68), p(50, 88), p(18, 68), p(18, 40)},
			closed: true,
			width:  2.6,
		},
		{points: []point{p(50, 32), p(50, 88)}, width: 1.8},            // 竖向切割线
		{points: []point{p(18, 40), p(50, 60), p(82, 40)}, width: 1.8}, // V 形切割线
	}

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			hits := 0
			for sy := 0; sy < subsamples; sy++ {
				for sx := 0; sx < subsamples; sx++ {
					q := point{float64(px) + (float64(sx)+0.5)/subsamples, float64(py) + (float64(sy)+0.5)/subsamples}
					if covered(q, strokes) {
						hits++
					}
				}
			}
			alpha := uint8(math.Round(255 * float64(hits) / subsamples / subsamples))
			img.SetNRGBA(px, py, color.NRGBA{A: alpha})
		}
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", *out)
}

// covered 判断采样点是否落在任一描边内（圆头圆角，等价于到线段距离 ≤ 半线宽）。
func covered(q point, strokes []stroke) bool {
	for _, s := range strokes {
		n := len(s.points)
		segs := n - 1
		if s.closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			if distToSegment(q, s.points[i], s.points[(i+1)%n]) <= s.width/2 {
				return true
			}
		}
	}
	return false
}

func distToSegment(q, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := ((q.x-a.x)*dx + (q.y-a.y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(q.x-(a.x+t*dx), q.y-(a.y+t*dy))
}
