package redact

import (
	"image"
	"runtime"
	"sync"
)

// blurRadius is the radius of the box blur for a picture of w by h pixels: a
// hundredth of its longer edge and at least eight pixels. Three passes come
// close to a Gaussian of about that sigma, which leaves no line of text in a
// screenshot readable.
func blurRadius(w, h int) int { return max(8, max(w, h)/100) }

// boxBlur returns src blurred by three passes of a box filter of radius r,
// horizontally and vertically. Edges are extended, so the border does not
// darken. The pixels stay premultiplied, which is what blurring wants.
func boxBlur(src *image.RGBA, r int) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	a := image.NewRGBA(src.Rect)
	copy(a.Pix, src.Pix)
	if w == 0 || h == 0 {
		return a
	}
	b := image.NewRGBA(src.Rect)
	stride := src.Stride
	for range 3 {
		parallel(h, func(y int) { blurLine(b.Pix[y*stride:], a.Pix[y*stride:], w, 4, r) })
		parallel(w, func(x int) { blurLine(a.Pix[x*4:], b.Pix[x*4:], h, stride, r) })
	}
	return a
}

// blurLine box filters n pixels that are step bytes apart from src into dst.
func blurLine(dst, src []byte, n, step, r int) {
	at := func(i int) int { return min(max(i, 0), n-1) * step }
	div := 2*r + 1
	var sum [4]int
	for i := -r; i <= r; i++ {
		o := at(i)
		for c := range 4 {
			sum[c] += int(src[o+c])
		}
	}
	for x := range n {
		o := x * step
		for c := range 4 {
			dst[o+c] = byte((sum[c] + div/2) / div)
		}
		add, sub := at(x+r+1), at(x-r)
		for c := range 4 {
			sum[c] += int(src[add+c]) - int(src[sub+c])
		}
	}
}

// parallel calls fn for 0 to n-1, in one contiguous run per processor, so
// that neighbouring columns do not share cache lines across goroutines.
func parallel(n int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := w * n / workers; i < (w+1)*n/workers; i++ {
				fn(i)
			}
		})
	}
	wg.Wait()
}
