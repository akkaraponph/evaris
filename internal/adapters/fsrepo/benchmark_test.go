package fsrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPrepareManifest(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			root, e := filepath.EvalSymlinks(b.TempDir())
			if e != nil {
				b.Fatal(e)
			}
			for i := 0; i < n; i++ {
				if e = os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.txt", i)), make([]byte, 128), 0600); e != nil {
					b.Fatal(e)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(n * 128))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, e = Snapshot(context.Background(), root); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
