package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                   string
	SupabaseURL            string
	SupabaseAnonKey        string
	SupabaseServiceRoleKey string
	InviteBaseURL          string
	CORSOrigins            string
	SMTPHost               string
	SMTPPort               string
	SMTPUsername           string
	SMTPPassword           string
	SMTPFrom               string
}

func Load() (Config, error) {
	_ = godotenv.Load()

	baseURL := os.Getenv("SUPABASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("SUPABASE_DB_URL")
	}
	if baseURL == "" {
		baseURL = "https://asscrzeikrmjpkwicxuo.supabase.co"
	}

	anonKey := os.Getenv("SUPABASE_ANON_KEY")
	if anonKey == "" {
		anonKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImFzc2NyemVpa3JtanBrd2ljeHVvIiwicm9sZSI6ImFub24iLCJpYXQiOjE3ODk2MDkxODQsImV4cCI6MjEwNTE4NTE4NH0.8SIVnhALw2ho23bFIQfgKFRFu9DtER9OfhCX18icfxQ"
	}

	serviceRoleKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	if serviceRoleKey == "" {
		serviceRoleKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImFzc2NyemVpa3JtanBrd2ljeHVvIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImlhdCI6MTc4OTYwOTE4NCwiZXhwIjoyMTA1MTg1MTg0fQ.X0fbx4siD_Wvd2TCfuojXXmqP2gvzUYUGdGeumajTpY"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	origins := os.Getenv("CORS_ORIGINS")
	if origins == "" {
		origins = "http://localhost:5173,http://127.0.0.1:5173,https://hr-gateway-lake.vercel.app"
	}

	return Config{
		Port:                   port,
		SupabaseURL:            baseURL,
		SupabaseAnonKey:        anonKey,
		SupabaseServiceRoleKey: serviceRoleKey,
		InviteBaseURL:          os.Getenv("INVITE_REDIRECT_URL"),
		CORSOrigins:            origins,
		SMTPHost:               envOrDefault("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:               envOrDefault("SMTP_PORT", "587"),
		SMTPUsername:           envOrDefault("SMTP_USERNAME", "sibabihutan5@gmail.com"),
		SMTPPassword:           envOrDefault("SMTP_PASSWORD", "twomrzdcxemvghgo"),
		SMTPFrom:               envOrDefault("SMTP_FROM", "Gateway HR <sibabihutan5@gmail.com>"),
	}, nil
}

func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
