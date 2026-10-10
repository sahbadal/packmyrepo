package app

import (
	"runtime"
	"strconv"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	loader "github.com/pkoukk/tiktoken-go-loader"
)

var (
	encOnce     sync.Once
	encoder     *tiktoken.Tiktoken
	encoderName = "o200k_base"
)

func setTokenizer(name string) {
	if name != "" {
		encoderName = name
	}
}

func getEncoder() *tiktoken.Tiktoken {
	encOnce.Do(func() {
		tiktoken.SetBpeLoader(loader.NewOfflineLoader())
		e, err := tiktoken.GetEncoding(encoderName)
		if err == nil {
			encoder = e
		}
	})
	return encoder
}

// countTokens returns the token count, falling back to a ~4 chars/token estimate.
func countTokens(s string) int {
	if s == "" {
		return 0
	}
	if e := getEncoder(); e != nil && len(s) < 4<<20 {
		return len(e.EncodeOrdinary(s))
	}
	return (len(s) + 3) / 4
}

// countAll fills f.Tokens for every file using all CPU cores.
func countAll(files []*File) {
	getEncoder() // initialise once before fanning out
	jobs := make(chan *File)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				f.Tokens = countTokensCached(f.Content)
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
}

func itoa(n int) string { return strconv.Itoa(n) }

// humanTokens formats 12345 as "12.3k".
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1_000_000, 'f', 2, 64) + "M"
	case n >= 10_000:
		return strconv.Itoa(n/1000) + "k"
	case n >= 1000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(n)
}
