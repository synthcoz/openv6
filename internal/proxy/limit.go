package proxy

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// limiterFor returns a shared byte-rate limiter for a proxy (nil = unlimited).
// The rate applies to each direction separately.
func limiterFor(kbps int) *rate.Limiter {
	if kbps <= 0 {
		return nil
	}
	bps := kbps * 1024
	burst := bps
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return rate.NewLimiter(rate.Limit(bps), burst)
}

// copyLimited copies like io.Copy, waiting on lim for every chunk.
// Without a limiter it falls back to io.Copy (zero-copy splice on Linux).
func copyLimited(dst io.Writer, src io.Reader, lim *rate.Limiter) (int64, error) {
	if lim == nil {
		return io.Copy(dst, src)
	}
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if err := lim.WaitN(context.Background(), n); err != nil {
				return total, err
			}
			w, werr := dst.Write(buf[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return total, nil
			}
			return total, rerr
		}
	}
}
