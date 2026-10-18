// Command gen-icons 生成 macOS 用的两张图标，图形都取自 web/public/favicon.svg
// 的外轮廓和内部切割线：
//
//   - 菜单栏模板图标：只用黑色 + 透明度表达形状，由系统按深色/浅色菜单栏
//     自动着色，所以不能直接复用 Windows 托盘那张彩色图标。
//
//   - 应用图标：macOS 标准圆角方形底 + favicon 的蓝紫渐变 + 白色描边，
//     打包时由 build-app.sh 转成 .icns。
//
//     go run ./scripts/macos/gen-icons
//     go run ./scripts/macos/gen-icons -tray /tmp/tray.png -app /tmp/app.png
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

type point struct{ x, y float64 }

type stroke struct {
	points []point
	closed bool
	width  float64 // 像素
	alpha  float64 // 0..1
}

// catStrokes 把 favicon.svg 的图形（viewBox (14,14)-(86,92)）缩放到 boxSize 大小，
// 中心放在 (cx, cy)。widths 依次是外轮廓和切割线的线宽（像素）。
func catStrokes(cx, cy, boxSize, outlineWidth, lineWidth, lineAlpha float64) []stroke {
	scale := boxSize / 78
	p := func(x, y float64) point { return point{cx + (x-50)*scale, cy + (y-53)*scale} }
	return []stroke{
		{ // 外轮廓
			points: []point{p(32, 18), p(44, 32), p(56, 32), p(68, 18), p(82, 40), p(82, 68), p(50, 88), p(18, 68), p(18, 40)},
			closed: true,
			width:  outlineWidth,
			alpha:  1,
		},
		{points: []point{p(50, 32), p(50, 88)}, width: lineWidth, alpha: lineAlpha},            // 竖向切割线
		{points: []point{p(18, 40), p(50, 60), p(82, 40)}, width: lineWidth, alpha: lineAlpha}, // V 形切割线
	}
}

func main() {
	trayOut := flag.String("tray", "cmd/prismcat/tray_icon_darwin.png", "菜单栏模板图标输出路径")
	appOut := flag.String("app", "scripts/macos/AppIcon.png", "应用图标输出路径")
	flag.Parse()

	writePNG(*trayOut, trayIcon())
	writePNG(*appOut, appIcon())
}

// trayIcon: systray 以 16pt 显示菜单栏图标，Retina 下对应 32px。
func trayIcon() image.Image {
	const size = 32
	strokes := catStrokes(size/2, size/2, 29, 2.6, 1.8, 1)
	return render(size, 8, func(q point) color.NRGBA {
		return color.NRGBA{A: uint8(255 * strokeCoverage(q, strokes))}
	})
}

// appIcon: 1024px 画布，按 Apple 图标网格留边，主体是 824px 的圆角方形。
func appIcon() image.Image {
	const size = 1024
	const body = 824
	const margin = (size - body) / 2
	top := color.NRGBA{0x3B, 0x82, 0xF6, 0xFF}    // favicon 渐变起点
	bottom := color.NRGBA{0x8B, 0x5C, 0xF6, 0xFF} // favicon 渐变终点
	strokes := catStrokes(size/2, size/2+8, 520, 34, 22, 0.55)

	return render(size, 4, func(q point) color.NRGBA {
		if !inSquircle(q, margin, body) {
			return color.NRGBA{}
		}
		// 左上到右下的线性渐变，再叠白色描边。
		t := ((q.x - margin) + (q.y - margin)) / (2 * body)
		bg := lerp(top, bottom, math.Max(0, math.Min(1, t)))
		return lerp(bg, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}, strokeCoverage(q, strokes))
	})
}

// inSquircle 用超椭圆 |x|^5 + |y|^5 <= 1 近似 macOS 图标的连续圆角方形。
func inSquircle(q point, margin, body float64) bool {
	half := body / 2
	x := math.Abs(q.x-margin-half) / half
	y := math.Abs(q.y-margin-half) / half
	return math.Pow(x, 5)+math.Pow(y, 5) <= 1
}

// render 对每个像素做 samples×samples 次采样取平均，实现抗锯齿。
func render(size, samples int, shade func(point) color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	n := float64(samples * samples)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			// 按预乘 alpha 累加，避免透明边缘混入黑色。
			var r, g, b, a float64
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					c := shade(point{float64(px) + (float64(sx)+0.5)/float64(samples), float64(py) + (float64(sy)+0.5)/float64(samples)})
					ca := float64(c.A) / 255
					r += float64(c.R) * ca
					g += float64(c.G) * ca
					b += float64(c.B) * ca
					a += ca
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(r / a)),
				G: uint8(math.Round(g / a)),
				B: uint8(math.Round(b / a)),
				A: uint8(math.Round(255 * a / n)),
			})
		}
	}
	return img
}

// strokeCoverage 返回采样点被描边覆盖的不透明度（圆头圆角，等价于到线段距离 ≤ 半线宽）。
func strokeCoverage(q point, strokes []stroke) float64 {
	cover := 0.0
	for _, s := range strokes {
		n := len(s.points)
		segs := n - 1
		if s.closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			if distToSegment(q, s.points[i], s.points[(i+1)%n]) <= s.width/2 {
				cover = math.Max(cover, s.alpha)
				break
			}
		}
	}
	return cover
}

func distToSegment(q, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := ((q.x-a.x)*dx + (q.y-a.y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(q.x-(a.x+t*dx), q.y-(a.y+t*dy))
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), mix(a.A, b.A)}
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s", path)
}
