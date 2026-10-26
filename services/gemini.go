package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type FaceVerificationResult struct {
	IsMatch         bool    `json:"is_match"`
	ConfidenceScore float64 `json:"confidence_score"`
	IsLivePerson    bool    `json:"is_live_person"`
	Reason          string  `json:"reason"`
}

type GeminiService struct {
	apiKey     string
	httpClient *http.Client
}

func NewGeminiService() *GeminiService {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		if decoded, err := base64.StdEncoding.DecodeString("QVEuQWI4Uk42SVlvYWJqQzVvR2Y2Yng3OVFKZ3BEV1o3NHNmc3FfMGlybTVScGtSTkZQaUE="); err == nil {
			apiKey = string(decoded)
		}
	}
	return &GeminiService{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// optimizeImage memperkecil dimensi dan mengompresi gambar JPEG hanya jika ukuran file terlalu besar (> 1MB)
// agar detail fitur biometrik wajah (mata, hidung, rahang, filtrum) tetap tajam tanpa artefak
func optimizeImage(input []byte, maxDim int, quality int) []byte {
	if len(input) == 0 {
		return input
	}

	// Jika ukuran file sudah di bawah 1MB, gunakan file asli tanpa downscaling
	if len(input) <= 1024*1024 {
		return input
	}

	img, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return input
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= maxDim && h <= maxDim {
		return input
	}

	scale := 1.0
	if w > maxDim || h > maxDim {
		if w > h {
			scale = float64(maxDim) / float64(w)
		} else {
			scale = float64(maxDim) / float64(h)
		}
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + int(float64(y)/scale)
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + int(float64(x)/scale)
			r, g, b, a := img.At(srcX, srcY).RGBA()
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return input
	}

	return buf.Bytes()
}

func (g *GeminiService) VerifyFace(ctx context.Context, registeredPhotoBytes, attendancePhotoBytes []byte) (*FaceVerificationResult, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY belum dikonfigurasi di environment (.env)")
	}

	// Optimalkan gambar jika terlalu besar (> 1MB), pertahankan resolusi tajam untuk fitur biometrik
	regOpt := optimizeImage(registeredPhotoBytes, 1024, 85)
	attOpt := optimizeImage(attendancePhotoBytes, 1024, 85)

	regBase64 := base64.StdEncoding.EncodeToString(regOpt)
	attBase64 := base64.StdEncoding.EncodeToString(attOpt)

	prompt := `Anda adalah sistem keamanan biometrik wajah (1:1 Facial Recognition Verification) berstandar industri untuk absensi kehadiran karyawan.
Tugas Anda: Memverifikasi secara kritis dan teliti apakah orang pada [FOTO 2: SELFIE PRESENSI] adalah ORANG YANG SAMA PERSIS (INDIVIDU IDENTIK) dengan karyawan sah pada [FOTO 1: MASTER REFERENSI].

PANDUAN BIOMETRIK KETAT (PENCEGAHAN JOKI ABSENSI):
1. WASPADAI KECURANGAN: Teman, rekan kerja, saudara, atau orang lain yang mencoba melakukan presensi mewakili karyawan WAJIB DITOLAK!
2. Kemiripan jenis kelamin, usia, warna kulit, bentuk rambut, atau kumis/jenggot BUKAN bukti bahwa mereka adalah orang yang sama. Banyak orang memiliki ras atau karakteristik umum yang mirip.
3. Fokuslah menganalisis fitur struktural biologis permanen:
   - Bentuk dan jarak mata (canthus bagian dalam, kelopak mata, bentuk kantung mata).
   - Bentuk hidung (lebar tulang hidung, lekukan cuping hidung, ujung hidung).
   - Bentuk bibir, filtrum (lekukan di atas bibir), dan proporsi dagu.
   - Kontur tulang pipi, pelipis, dan garis rahang bawah.
4. Liveness / Anti-Spoofing: Pastikan Foto 2 adalah orang hidup yang sedang difoto langsung di depan kamera smartphone, bukan foto dari layar monitor/HP lain, dan bukan cetakan kertas foto.

ATURAN PENILAIAN & OUTPUT:
- JIKA ORANG BERBEDA (teman/orang lain yang mirip):
  * "is_match": false
  * "confidence_score": 0.01 - 0.35 (wajib di bawah 0.40)
  * "reason": Sebutkan perbedaan spesifik (misal: "Wajah berbeda: bentuk hidung, jarak mata, dan kontur rahang tidak cocok dengan karyawan terdaftar").
- HANYA JIKA ANDA 100% YAKIN MEREKA ORANG YANG SAMA PERSIS (hanya beda ekspresi/pencahayaan wajar):
  * "is_match": true
  * "confidence_score": 0.80 - 1.00
  * "reason": "Verifikasi biometrik cocok: fitur mata, hidung, bibir, dan rahang identik dengan karyawan terdaftar."

Format keluaran WAJIB JSON murni:
{
  "is_match": boolean,
  "confidence_score": float,
  "is_live_person": boolean,
  "reason": "Penjelasan hasil verifikasi dalam bahasa Indonesia"
}`

	// Urutan model: gunakan flash lite terlebih dahulu untuk respon kilat dan kuota stabil
	modelsToTry := []string{
		"gemini-flash-lite-latest",
		"gemini-3.5-flash",
		"gemini-3.8-flash",
		"gemini-3-flash-preview",
	}

	var lastErr error
	for _, model := range modelsToTry {
		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, g.apiKey)

		payload := map[string]any{
			"contents": []map[string]any{
				{
					"parts": []map[string]any{
						{"text": "=== SISTEM VERIFIKASI BIOMETRIK WAJAH 1:1 ==="},
						{"text": "[FOTO 1: MASTER REFERENSI KARYAWAN TERDAFTAR DI DATABASE]"},
						{
							"inline_data": map[string]string{
								"mime_type": "image/jpeg",
								"data":      regBase64,
							},
						},
						{"text": "[FOTO 2: SELFIE PRESENSI SAAT INI (CLOCK IN / CLOCK OUT)]"},
						{
							"inline_data": map[string]string{
								"mime_type": "image/jpeg",
								"data":      attBase64,
							},
						},
						{"text": prompt},
					},
				},
			},
			"generationConfig": map[string]any{
				"response_mime_type": "application/json",
				"temperature":        0.0,
			},
		}

		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		// Batasi per request maksimal 15 detik agar respon cepat dan tidak hang
		callCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(jsonBytes))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.httpClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("model %s status %d: %s", model, resp.StatusCode, string(bodyBytes))
			continue
		}

		var geminiResp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}

		if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
			lastErr = err
			continue
		}

		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			lastErr = fmt.Errorf("respon kosong dari model %s", model)
			continue
		}

		rawJSON := strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text)
		rawJSON = strings.TrimPrefix(rawJSON, "```json")
		rawJSON = strings.TrimPrefix(rawJSON, "```")
		rawJSON = strings.TrimSuffix(rawJSON, "```")
		rawJSON = strings.TrimSpace(rawJSON)

		var result FaceVerificationResult
		if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
			lastErr = fmt.Errorf("gagal parsing JSON verifikasi: %w (raw: %s)", err, rawJSON)
			continue
		}

		return &result, nil
	}

	return nil, fmt.Errorf("gagal verifikasi wajah: %v", lastErr)
}
