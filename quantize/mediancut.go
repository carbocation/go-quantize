// Package quantize offers an implementation of the draw.Quantize interface using an optimized Median Cut method,
// including advanced functionality for fine-grained control of color priority
package quantize

import (
	"image"
	"image/color"
)

type colorAxis uint8

// Color axis constants
const (
	red colorAxis = iota
	green
	blue
)

type simpleColor struct {
	r uint8
	g uint8
	b uint8
}

func (c simpleColor) RGBA() (r, g, b, a uint32) {
	r = uint32(c.r)
	r |= r << 8
	g = uint32(c.g)
	g |= g << 8
	b = uint32(c.b)
	b |= b << 8
	a = 0xFFFF
	return
}

// gtColor returns if color a is greater than color b on the specified color channel
func (c simpleColor) gt(other simpleColor, span colorAxis) bool {
	switch span {
	case red:
		return c.r > other.r
	case green:
		return c.g > other.g
	default:
		return c.b > other.b
	}
}

func simpleFromGeneral(general color.Color) simpleColor {
	r, g, b, _ := general.RGBA()
	return simpleColor{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func simpleFromYCbCr(general color.YCbCr) simpleColor {
	r, g, b := color.YCbCrToRGB(general.Y, general.Cb, general.Cr)
	return simpleColor{r, g, b}
}

func simpleFromRGBA(general color.RGBA) simpleColor {
	return simpleColor{general.R, general.G, general.B}
}

type colorPriority struct {
	p uint32
	simpleColor
}

type colorBucket []colorPriority

func (c colorBucket) partition(mean simpleColor, span colorAxis) (colorBucket, colorBucket) {
	left, right := 0, len(c)-1
	for left < right {
		for mean.gt(c[left].simpleColor, span) {
			left++
		}
		for !mean.gt(c[right].simpleColor, span) {
			right--
		}
		c[left], c[right] = c[right], c[left]
	}
	return c[:left], c[left:]
}

// AggregationType specifies the type of aggregation to be done
type AggregationType uint8

const (
	// Mode - pick the highest priority value
	Mode AggregationType = iota
	// Mean - weighted average all values
	Mean
)

// MedianCutQuantizer implements the go draw.Quantizer interface using the Median Cut method
type MedianCutQuantizer struct {
	// The type of aggregation to be used to find final colors
	Aggregation AggregationType
	// The weighting function to use on each pixel
	Weighting func(image.Image, int, int) uint32
	// Whether to create a transparent entry
	AddTransparent bool
}

// colorSpan performs linear color bucket statistics
func colorSpan(colors []colorPriority) (mean simpleColor, span colorAxis) {
	var r, g, b uint64    // Sum of channels
	var r2, g2, b2 uint64 // Sum of square of channels
	var priority uint64

	for _, c := range colors { // Calculate priority-weighted sums
		priority += uint64(c.p)
		r += uint64(uint32(c.r) * c.p)
		g += uint64(uint32(c.g) * c.p)
		b += uint64(uint32(c.b) * c.p)
		r2 += uint64(uint32(c.r*c.r) * c.p)
		g2 += uint64(uint32(c.g*c.g) * c.p)
		b2 += uint64(uint32(c.b*c.b) * c.p)
	}

	mr := (r + priority - 1) / priority
	mg := (g + priority - 1) / priority
	mb := (b + priority - 1) / priority
	mean = simpleColor{uint8(mr), uint8(mg), uint8(mb)}

	sr := r2/priority - mr*mr // Calculate the variance to find which span is the broadest
	sg := g2/priority - mg*mg
	sb := b2/priority - mb*mb
	if sr > sg && sr > sb {
		span = red
	} else if sg > sb {
		span = green
	} else {
		span = blue
	}
	return
}

//bucketize takes a bucket and performs median cut on it to obtain the target number of grouped buckets
func bucketize(colors colorBucket, num int) (buckets []colorBucket) {
	if len(colors) == 0 || num == 0 {
		return nil
	}
	bucket := colors
	buckets = make([]colorBucket, 1, num*2)
	buckets[0] = bucket

	for len(buckets) < num && len(buckets) < len(colors) { // Limit to palette capacity or number of colors
		bucket, buckets = buckets[0], buckets[1:]
		if len(bucket) < 2 {
			buckets = append(buckets, bucket)
			continue
		}
		mean, span := colorSpan(bucket)

		left, right := bucket.partition(mean, span)
		buckets = append(buckets, left, right)
	}
	return
}

// palettize finds a single color to represent a set of color buckets
func (q MedianCutQuantizer) palettize(p color.Palette, buckets []colorBucket) color.Palette {
	for _, bucket := range buckets {
		switch q.Aggregation {
		case Mean:
			mean, _ := colorSpan(bucket)
			p = append(p, mean)
		case Mode:
			var best *colorPriority
			for _, c := range bucket {
				if best == nil || c.p > best.p {
					best = &c
				}
			}
			p = append(p, best)
		}
	}
	return p
}

// quantizeSlice expands the provided bucket and then palettizes the result
func (q MedianCutQuantizer) quantizeSlice(p color.Palette, colors []colorPriority) color.Palette {
	numColors := cap(p) - len(p)
	addTransparent := q.AddTransparent
	if addTransparent {
		for _, c := range p {
			if _, _, _, a := c.RGBA(); a == 0 {
				addTransparent = false
			}
		}
		if addTransparent {
			numColors--
		}
	}
	buckets := bucketize(colors, numColors)
	p = q.palettize(p, buckets)
	if addTransparent {
		p = append(p, color.RGBA{0, 0, 0, 0})
	}
	return p
}

func simpleYCbCrAt(m *image.YCbCr, x int, y int) simpleColor {
	return simpleFromYCbCr(m.YCbCrAt(x, y))
}

func simpleRGBAAt(m *image.RGBA, x int, y int) simpleColor {
	return simpleFromRGBA(m.RGBAAt(x, y))
}

func simpleAt(m image.Image, x int, y int) simpleColor {
	switch i := m.(type) {
	case *image.YCbCr:
		return simpleYCbCrAt(i, x, y)
	case *image.RGBA:
		return simpleRGBAAt(i, x, y)
	default:
		return simpleFromGeneral(m.At(x, y))
	}
}

// buildBucket creates a prioritized color slice with all the colors in the image
func (q MedianCutQuantizer) buildBucket(m image.Image) (bucket []colorPriority) {
	bounds := m.Bounds()
	size := (bounds.Max.X - bounds.Min.X) * (bounds.Max.Y - bounds.Min.Y)
	sparseBucket := make([]colorPriority, size)
	created := 0

	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			priority := uint32(1)
			if q.Weighting != nil {
				priority = q.Weighting(m, x, y)
			}
			if priority != 0 {
				c := simpleAt(m, x, y)
				index := int(c.r)<<16 | int(c.g)<<8 | int(c.b)
				for i := 1; ; i++ {
					if sparseBucket[index%size].p == 0 {
						sparseBucket[index%size] = colorPriority{priority, c}
						created++
						break
					}
					if sparseBucket[index%size].simpleColor == c {
						sparseBucket[index%size].p += priority
						break
					}
					index += 1 + i
				}
			}
		}
	}
	bucket = sparseBucket[:0]
	for _, p := range sparseBucket {
		if p.p != 0 {
			bucket = append(bucket, p)
		}
	}
	return
}

// Quantize quantizes an image to a palette and returns the palette
func (q MedianCutQuantizer) Quantize(p color.Palette, m image.Image) color.Palette {
	return q.quantizeSlice(p, q.buildBucket(m))
}
