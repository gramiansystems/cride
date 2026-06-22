package highlight

import (
	"strconv"
	"sync"
	"testing"
)

func TestLineCacheHitMatchesMiss(t *testing.T) {
	t.Parallel()

	h := New()
	line := `func main() { fmt.Println("hi") }`
	miss := h.Line("main.go", line)
	hit := h.Line("main.go", line)
	if miss != hit {
		t.Fatalf("cache hit differs from miss:\n%q\n%q", miss, hit)
	}

	// Identical lines share entries across files with the same lexer.
	other := h.Line("other.go", line)
	if other != miss {
		t.Fatalf("same-language file produced different output:\n%q\n%q", other, miss)
	}
	if got := h.cache.len(); got != 1 {
		t.Fatalf("cache entries = %d, want 1 shared entry", got)
	}
}

func TestLineCacheBoundRespected(t *testing.T) {
	t.Parallel()

	h := New()
	h.cache = newLineCache(8)
	for i := 0; i < 50; i++ {
		h.Line("main.go", "var x"+strconv.Itoa(i)+" int")
	}
	if got := h.cache.len(); got > 8 {
		t.Fatalf("cache grew to %d entries, bound is 8", got)
	}

	// Eviction must not change output correctness.
	want := h.highlight(h.lexerFor("main.go"), "var x0 int")
	if got := h.Line("main.go", "var x0 int"); got != want {
		t.Fatalf("re-highlight after eviction differs:\n%q\n%q", got, want)
	}
}

func TestHighlighterConcurrentAccess(t *testing.T) {
	t.Parallel()

	h := New()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.Line("main.go", "x := "+strconv.Itoa(i%20))
				h.Line("script.py", "x = "+strconv.Itoa(i%20))
			}
		}(g)
	}
	wg.Wait()
}
