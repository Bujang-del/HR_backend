package config

import (
	"fmt"
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
}

func Load() (Config, error) {
	_ = godotenv.Load()

	baseURL := os.Getenv("SUPABASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("SUPABASE_DB_URL")
	}
	if baseURL == "" || os.Getenv("SUPABASE_ANON_KEY") == "" {
		return Config{}, fmt.Errorf("SUPABASE_URL/SUPABASE_DB_URL and SUPABASE_ANON_KEY are required")
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
		SupabaseAnonKey:        os.Getenv("SUPABASE_ANON_KEY"),
		SupabaseServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
		InviteBaseURL:          os.Getenv("INVITE_REDIRECT_URL"),
		CORSOrigins:            origins,
	}, nil
}
