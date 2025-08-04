// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"integration/app/plugin/types"
	"path/filepath"
	"strings"
	"testing"
)

// Benchmark the optimized getHash function
func BenchmarkGetHashOptimized(b *testing.B) {
	hashTypes := []string{types.Md5, types.SHA1, types.SHA256, types.SHA512, types.GitHash}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, hashType := range hashTypes {
			_, err := getHash(hashType, 1024)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Benchmark file extension extraction optimization
func BenchmarkFileExtraction(b *testing.B) {
	filenames := []string{
		"document.pdf",
		"script.py", 
		"data.csv",
		"archive.tar.gz",
		"image.jpeg",
		"config.json",
		"readme.md",
		"program.cpp",
		"style.css",
		"page.html",
	}
	
	b.Run("Optimized", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, filename := range filenames {
				ext := filepath.Ext(filename)
				if len(ext) > 0 {
					_ = ext[1:] // Remove the dot
				}
			}
		}
	})
	
	b.Run("Original", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, filename := range filenames {
				parts := strings.Split(filename, ".")
				if len(parts) > 1 {
					_ = parts[len(parts)-1]
				}
			}
		}
	})
}