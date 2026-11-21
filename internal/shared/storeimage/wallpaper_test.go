package storeimage

import (
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleWallpaper = "/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.png"

func TestVerifyWallpaperAcceptsLocalPNGJPEGAndWebP(t *testing.T) {
	root := t.TempDir()
	restore := useRoot(t, root)
	defer restore()

	pngPath := sampleWallpaper
	writeImage(t, root, pngPath, func(path string) {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
			t.Fatal(err)
		}
	})
	if got, err := VerifyWallpaper("  " + pngPath + "  "); err != nil || got != pngPath {
		t.Fatalf("png wallpaper: got %q err %v", got, err)
	}

	jpegPath := "/uploads/reseller/2026/11/22222222-2222-4222-8222-222222222222.jpg"
	writeImage(t, root, jpegPath, func(path string) {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := jpeg.Encode(file, image.NewRGBA(image.Rect(0, 0, 6, 3)), nil); err != nil {
			t.Fatal(err)
		}
	})
	if got, err := VerifyWallpaper(jpegPath); err != nil || got != jpegPath {
		t.Fatalf("jpeg wallpaper: got %q err %v", got, err)
	}

	webpPath := "/uploads/wallpaper/2026/11/33333333-3333-4333-8333-333333333333.webp"
	writeImage(t, root, webpPath, func(path string) {
		// VP8X：宽 2，高 3。校验只认容器头，不把文件当图片解码器去执行。
		payload := []byte("RIFF")
		payload = append(payload, 22, 0, 0, 0)
		payload = append(payload, []byte("WEBPVP8X")...)
		payload = append(payload, 10, 0, 0, 0)
		payload = append(payload, 0, 0, 0, 0, 1, 0, 0, 2, 0, 0)
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if got, err := VerifyWallpaper(webpPath); err != nil || got != webpPath {
		t.Fatalf("webp wallpaper: got %q err %v", got, err)
	}
	if PublicWallpaper(webpPath) != webpPath {
		t.Fatalf("public path dropped a valid wallpaper")
	}
}

func TestVerifyWallpaperRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	restore := useRoot(t, root)
	defer restore()

	cases := []string{
		"",
		"   ",
	}
	for _, raw := range cases {
		if got, err := VerifyWallpaper(raw); err != nil || got != "" {
			t.Fatalf("empty wallpaper should clear, got %q err %v", got, err)
		}
	}

	rejected := []string{
		"https://cdn.example.test/wall.jpg",
		"http://127.0.0.1/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.png",
		"/uploads/common/2026/11/../../../../etc/passwd",
		"/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.png/../../secret.png",
		"/uploads/telegram/2026/11/11111111-1111-4111-8111-111111111111.png",
		"/uploads/common/2026/11/not-a-uuid.png",
		"/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.svg",
		"/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.gif",
		`/uploads/common/2026/11/11111111-1111-4111-8111-111111111111.png?x=1`,
	}
	for _, raw := range rejected {
		if _, err := VerifyWallpaper(raw); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
		if PublicWallpaper(raw) != "" {
			t.Fatalf("public path kept unsafe value %s", raw)
		}
	}

	svgPath := "/uploads/common/2026/11/44444444-4444-4444-8444-444444444444.png"
	writeImage(t, root, svgPath, func(path string) {
		body := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := VerifyWallpaper(svgPath); err == nil {
		t.Fatal("svg renamed to png was accepted")
	}

	exePath := "/uploads/common/2026/11/55555555-5555-4555-8555-555555555555.jpg"
	writeImage(t, root, exePath, func(path string) {
		if err := os.WriteFile(path, []byte("MZ\x90\x00executable"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := VerifyWallpaper(exePath); err == nil {
		t.Fatal("executable renamed to jpg was accepted")
	}

	mismatch := "/uploads/common/2026/11/66666666-6666-4666-8666-666666666666.jpg"
	writeImage(t, root, mismatch, func(path string) {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := VerifyWallpaper(mismatch); err == nil {
		t.Fatal("png bytes with a jpg name were accepted")
	}

	huge := "/uploads/common/2026/11/77777777-7777-4777-8777-777777777777.png"
	writeImage(t, root, huge, func(path string) {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, MaxWallpaperEdge+1, 1))); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := VerifyWallpaper(huge); err == nil {
		t.Fatal("oversized image was accepted")
	}

	linkName := "/uploads/common/2026/11/88888888-8888-4888-8888-888888888888.png"
	targetDir := filepath.Join(root, "common", "2026", "11")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	file, err := os.Create(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Symlink(outside, filepath.Join(root, strings.TrimPrefix(linkName, "/uploads/"))); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyWallpaper(linkName); err == nil {
		t.Fatal("symlink wallpaper was accepted")
	}
}

func useRoot(t *testing.T, root string) func() {
	t.Helper()
	prev := Root
	Root = root
	return func() { Root = prev }
}

func writeImage(t *testing.T, root, publicPath string, write func(path string)) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(publicPath, "/uploads/")))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	write(full)
}
