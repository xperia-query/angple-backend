package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	pkglogger "github.com/damoang/angple-backend/pkg/logger"
	"github.com/damoang/angple-backend/pkg/storage"

	gnurepo "github.com/damoang/angple-backend/internal/repository/gnuboard"
)

const (
	maxProfileImageSize = 2 * 1024 * 1024 // 2MB
	profileMaxWidth     = 200
)

// MemberService handles member profile image operations
type MemberService struct {
	s3         *storage.S3Client
	memberRepo gnurepo.MemberRepository
}

// NewMemberService creates a new MemberService
func NewMemberService(s3Client *storage.S3Client, memberRepo gnurepo.MemberRepository) *MemberService {
	return &MemberService{
		s3:         s3Client,
		memberRepo: memberRepo,
	}
}

// UpdateMemberImage processes and uploads a member profile image
func (s *MemberService) UpdateMemberImage(ctx context.Context, mbID string, file *multipart.FileHeader) (string, error) {
	if file.Size > maxProfileImageSize {
		return "", fmt.Errorf("파일 크기가 너무 큽니다 (최대 2MB)")
	}

	ext := strings.ToLower(path.Ext(file.Filename))
	if !isProfileImageExt(ext) {
		return "", fmt.Errorf("지원하지 않는 이미지 형식입니다: %s", ext)
	}

	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("파일 열기 실패: %w", err)
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		return "", fmt.Errorf("파일 읽기 실패: %w", err)
	}

	// Content-Type 검증
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("이미지 파일이 아닙니다")
	}

	// 이미지 디코딩 및 리사이즈
	var reader io.Reader
	var size int64

	if ext == ".gif" {
		// GIF는 리사이즈 없이 그대로 업로드
		reader = bytes.NewReader(data)
		size = int64(len(data))
	} else {
		img, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			return "", fmt.Errorf("이미지 디코딩 실패: %w", decErr)
		}

		// 리사이즈 (200x200 이내)
		if img.Bounds().Dx() > profileMaxWidth || img.Bounds().Dy() > profileMaxWidth {
			img = resizeProfileImage(img, profileMaxWidth)
		}

		// JPEG로 재인코딩
		var buf bytes.Buffer
		if ext == ".png" {
			if err := png.Encode(&buf, img); err != nil {
				return "", fmt.Errorf("이미지 인코딩 실패: %w", err)
			}
			contentType = "image/png"
		} else {
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
				return "", fmt.Errorf("이미지 인코딩 실패: %w", err)
			}
			contentType = "image/jpeg"
			ext = ".jpg"
		}
		reader = &buf
		size = int64(buf.Len())
	}

	// 기존 이미지 삭제
	member, err := s.memberRepo.FindByID(mbID)
	if err != nil {
		return "", fmt.Errorf("회원 조회 실패: %w", err)
	}
	if member.MbImageUrl != "" {
		_ = s.s3.Delete(ctx, member.MbImageUrl)
	}

	// S3 업로드
	prefix := mbID[:2]
	if len(mbID) < 2 {
		prefix = mbID
	}
	key := fmt.Sprintf("data/member_image/%s/%s_%d%s",
		strings.ToLower(prefix), mbID, time.Now().Unix(), ext)

	result, err := s.s3.Upload(ctx, key, reader, contentType, size)
	if err != nil {
		return "", fmt.Errorf("S3 업로드 실패: %w", err)
	}

	// DB 업데이트
	if err := s.memberRepo.UpdateMemberImageUrl(mbID, result.Key); err != nil {
		return "", fmt.Errorf("DB 업데이트 실패: %w", err)
	}

	cdnURL := result.CDNURL
	if cdnURL == "" {
		cdnURL = result.URL
	}

	pkglogger.GetLogger().Info().
		Str("mb_id", mbID).
		Str("key", result.Key).
		Int64("size", size).
		Msg("member profile image uploaded")

	return cdnURL, nil
}

// DeleteMemberImage removes a member's profile image
func (s *MemberService) DeleteMemberImage(ctx context.Context, mbID string) error {
	member, err := s.memberRepo.FindByID(mbID)
	if err != nil {
		return fmt.Errorf("회원 조회 실패: %w", err)
	}

	if member.MbImageUrl != "" {
		if delErr := s.s3.Delete(ctx, member.MbImageUrl); delErr != nil {
			pkglogger.GetLogger().Warn().
				Str("mb_id", mbID).
				Str("key", member.MbImageUrl).
				Err(delErr).
				Msg("failed to delete profile image from S3")
		}
	}

	if err := s.memberRepo.ClearMemberImageUrl(mbID); err != nil {
		return fmt.Errorf("DB 업데이트 실패: %w", err)
	}

	pkglogger.GetLogger().Info().
		Str("mb_id", mbID).
		Msg("member profile image deleted")

	return nil
}

func isProfileImageExt(ext string) bool {
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	}
	return false
}

// resizeProfileImage resizes an image to fit within maxSize x maxSize
func resizeProfileImage(img image.Image, maxSize int) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// 긴 변 기준으로 리사이즈
	var newW, newH int
	if w >= h {
		newW = maxSize
		newH = h * maxSize / w
	} else {
		newH = maxSize
		newW = w * maxSize / h
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := x * w / newW
			srcY := y * h / newH
			dst.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}
