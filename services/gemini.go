package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	return &GeminiService{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 25 * time.Second,
		},
	}
}

func (g *GeminiService) VerifyFace(ctx context.Context, registeredPhotoBytes, attendancePhotoBytes []byte) (*FaceVerificationResult, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY belum dikonfigurasi di environment (.env)")
	}

	regBase64 := base64.StdEncoding.EncodeToString(registeredPhotoBytes)
	attBase64 := base64.StdEncoding.EncodeToString(attendancePhotoBytes)

	prompt := `Anda adalah model AI pakar biometrik untuk verifikasi wajah sistem presensi kehadiran.
Tugas Anda:
Bandingkan dua gambar berikut secara seksama:
- Gambar 1 (Foto Master / Terdaftar): Foto referensi profil karyawan yang sah dan terdaftar di database.
- Gambar 2 (Foto Presensi / Selfie): Foto selfie yang baru saja diambil saat melakukan presensi (Clock In / Clock Out).

Kriteria Penilaian:
1. Analisis kecocokan biometrik: bentuk wajah, mata, hidung, garis bibir, dan kontur rahang.
2. Berikan toleransi wajar untuk perbedaan ekspresi (senyum/netral), pencahayaan ruangan, pemakaian kacamata, atau gaya rambut. Jika fitur struktur wajah sama, maka is_match = true.
3. Anti-Spoofing: Pastikan Gambar 2 diambil langsung dari orang nyata, bukan foto dari layar HP lain atau cetakan kertas.
4. Berikan confidence_score antara 0.0 (sama sekali tidak cocok) sampai 1.0 (sangat yakin orang yang sama).

Keluarkan HANYA JSON valid (tanpa markdown ataupun petik tambahan) dengan format:
{
  "is_match": boolean,
  "confidence_score": float,
  "is_live_person": boolean,
  "reason": "Penjelasan singkat dalam bahasa Indonesia"
}`

	modelsToTry := []string{"gemini-3.5-flash-lite", "gemini-3.8-flash"}

	var lastErr error
	for _, model := range modelsToTry {
		url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, g.apiKey)

		payload := map[string]any{
			"contents": []map[string]any{
				{
					"parts": []map[string]any{
						{"text": prompt},
						{
							"inline_data": map[string]string{
								"mime_type": "image/jpeg",
								"data":      regBase64,
							},
						},
						{
							"inline_data": map[string]string{
								"mime_type": "image/jpeg",
								"data":      attBase64,
							},
						},
					},
				},
			},
			"generationConfig": map[string]any{
				"response_mime_type": "application/json",
				"temperature":        0.1,
			},
		}

		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := g.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()

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
