package storeimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// MaxWallpaperBytes 是店铺壁纸的大小上限，低于默认上传上限。
	MaxWallpaperBytes int64 = 8 << 20
	// MaxWallpaperEdge 是壁纸最长边，和上传配置的图片边长上限一致。
	MaxWallpaperEdge = 4096
)

// Root 是本地上传目录，与上传服务写入的 uploads 目录相同。测试可以改它。
var Root = "uploads"

// ErrInvalidWallpaper 表示壁纸路径、内容或尺寸不能用于店铺背景。
var ErrInvalidWallpaper = errors.New("invalid store wallpaper")

// 只接受本站上传目录里的 jpg/png/webp。不接受外链，避免把壁纸地址变成远端拉取。
var wallpaperPathPattern = regexp.MustCompile(`^/uploads/(product|post|banner|editor|common|category|reseller|wallpaper)/[0-9]{4}/[0-9]{2}/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\.(jpg|jpeg|png|webp)$`)

// PublicWallpaper 只保留可以公开给前台的本地图片地址。空值和非法值都变成空字符串。
func PublicWallpaper(raw string) string {
	value := strings.TrimSpace(raw)
	if !wallpaperPathPattern.MatchString(value) {
		return ""
	}
	return value
}

// VerifyWallpaper 校验壁纸。空字符串表示恢复默认或继承，不读文件。
// 非空时确认它是受控目录中的真实 jpg/png/webp，拒绝外链、路径穿越、可执行内容和 SVG。
func VerifyWallpaper(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	full, err := resolveWallpaper(value)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidWallpaper
	}
	if info.Size() <= 0 || info.Size() > MaxWallpaperBytes {
		return "", ErrInvalidWallpaper
	}
	root, err := filepath.Abs(Root)
	if err != nil {
		return "", ErrInvalidWallpaper
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", ErrInvalidWallpaper
	}
	realFile, err := filepath.EvalSymlinks(full)
	if err != nil || !insideDir(realRoot, realFile) {
		return "", ErrInvalidWallpaper
	}
	file, err := os.Open(full)
	if err != nil {
		return "", ErrInvalidWallpaper
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxWallpaperBytes+1))
	if err != nil || int64(len(data)) != info.Size() {
		return "", ErrInvalidWallpaper
	}
	if err := validateWallpaperBytes(value, data); err != nil {
		return "", err
	}
	return value, nil
}

func resolveWallpaper(publicPath string) (string, error) {
	if strings.TrimSpace(Root) == "" || !wallpaperPathPattern.MatchString(publicPath) {
		return "", ErrInvalidWallpaper
	}
	if strings.Contains(publicPath, "..") || strings.ContainsAny(publicPath, "\\?#") {
		return "", ErrInvalidWallpaper
	}
	root, err := filepath.Abs(Root)
	if err != nil {
		return "", ErrInvalidWallpaper
	}
	rel := strings.TrimPrefix(publicPath, "/uploads/")
	full := filepath.Join(root, filepath.FromSlash(rel))
	abs, err := filepath.Abs(full)
	if err != nil || !insideDir(root, abs) {
		return "", ErrInvalidWallpaper
	}
	return abs, nil
}

func insideDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func validateWallpaperBytes(publicPath string, data []byte) error {
	ext := strings.ToLower(filepath.Ext(publicPath))
	switch ext {
	case ".jpg", ".jpeg":
		if len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 || data[2] != 0xff {
			return ErrInvalidWallpaper
		}
	case ".png":
		if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
			return ErrInvalidWallpaper
		}
	case ".webp":
		if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
			return ErrInvalidWallpaper
		}
	default:
		return ErrInvalidWallpaper
	}
	width, height, err := wallpaperDimensions(ext, data)
	if err != nil || width <= 0 || height <= 0 || width > MaxWallpaperEdge || height > MaxWallpaperEdge {
		return ErrInvalidWallpaper
	}
	return nil
}

func wallpaperDimensions(ext string, data []byte) (int, int, error) {
	if ext == ".webp" {
		return decodeWebPDimensions(bytes.NewReader(data))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, ErrInvalidWallpaper
	}
	return cfg.Width, cfg.Height, nil
}

func decodeWebPDimensions(src io.Reader) (int, int, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(src, header); err != nil {
		return 0, 0, ErrInvalidWallpaper
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return 0, 0, ErrInvalidWallpaper
	}
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(src, chunkHeader); err != nil {
			return 0, 0, ErrInvalidWallpaper
		}
		chunkType := string(chunkHeader[0:4])
		chunkSize := int(binary.LittleEndian.Uint32(chunkHeader[4:8]))
		if chunkSize < 0 {
			return 0, 0, ErrInvalidWallpaper
		}
		data := make([]byte, chunkSize)
		if _, err := io.ReadFull(src, data); err != nil {
			return 0, 0, ErrInvalidWallpaper
		}
		switch chunkType {
		case "VP8X":
			if len(data) < 10 {
				return 0, 0, ErrInvalidWallpaper
			}
			width := 1 + int(data[4]) + int(data[5])<<8 + int(data[6])<<16
			height := 1 + int(data[7]) + int(data[8])<<8 + int(data[9])<<16
			return width, height, nil
		case "VP8 ":
			if len(data) < 10 {
				return 0, 0, ErrInvalidWallpaper
			}
			width := int(binary.LittleEndian.Uint16(data[6:8]) & 0x3FFF)
			height := int(binary.LittleEndian.Uint16(data[8:10]) & 0x3FFF)
			return width, height, nil
		case "VP8L":
			if len(data) < 5 || data[0] != 0x2f {
				return 0, 0, ErrInvalidWallpaper
			}
			bits := binary.LittleEndian.Uint32(data[1:5])
			width := int(bits&0x3FFF) + 1
			height := int((bits>>14)&0x3FFF) + 1
			return width, height, nil
		}
		if chunkSize%2 == 1 {
			if _, err := io.CopyN(io.Discard, src, 1); err != nil {
				return 0, 0, ErrInvalidWallpaper
			}
		}
	}
}
