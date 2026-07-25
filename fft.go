package main

import (
	"math"
	"math/cmplx"
)

func fft(a []complex128) []complex128 {
	n := len(a)
	if n <= 1 {
		return a
	}

	even := make([]complex128, n/2)
	odd := make([]complex128, n/2)
	for i := 0; i < n/2; i++ {
		even[i] = a[2*i]
		odd[i] = a[2*i+1]
	}

	even = fft(even)
	odd = fft(odd)

	out := make([]complex128, n)
	for k := 0; k < n/2; k++ {
		t := cmplx.Exp(complex(0, -2*math.Pi*float64(k)/float64(n))) * odd[k]
		out[k] = even[k] + t
		out[k+n/2] = even[k] - t
	}
	return out
}

func computeFFTMagnitudes(samples []float64) []float64 {
	n := len(samples)
	if n <= 1 {
		return nil
	}

	input := make([]complex128, n)
	for i, s := range samples {
		input[i] = complex(s, 0)
	}

	output := fft(input)

	mags := make([]float64, n/2)
	for i := 0; i < n/2; i++ {
		mags[i] = cmplx.Abs(output[i])
	}
	return mags
}
