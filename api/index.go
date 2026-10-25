package handler

import (
	"log"
	"net/http"
	"sync"
	"encoding/json"

	"hr-gateway-backend/config"
	"hr-gateway-backend/controllers"
	"hr-gateway-backend/routes"
	"hr-gateway-backend/services"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
)

var (
	fiberHandler http.HandlerFunc
	initOnce     sync.Once
)

func initialize() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("[Vercel Init Error] failed to load config: %v", err)
	}

	app := fiber.New(fiber.Config{
		ErrorHandler: controllers.ErrorHandler,
	})

	userClient := services.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseAnonKey)
	adminClient := services.NewSupabaseAdminClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	mailer := services.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom)

	routes.Register(app, userClient, adminClient, mailer, cfg.InviteBaseURL, cfg.CORSOrigins)

	fiberHandler = adaptor.FiberApp(app)
}

// Handler is the entrypoint for Vercel Serverless Functions
func Handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("inspect") == "1" || r.Header.Get("X-Inspect") == "1" {
		w.Header().Set("Content-Type", "application/json")
		import_data := map[string]interface{}{
			"url_path":    r.URL.Path,
			"url_string":  r.URL.String(),
			"request_uri": r.RequestURI,
			"headers":     r.Header,
		}
		json.NewEncoder(w).Encode(import_data)
		return
	}
	initOnce.Do(initialize)

	// Ensure Fiber parses the exact incoming URL
	if matched := r.Header.Get("X-Matched-Path"); matched != "" && matched != "/api" && matched != "/api/index.go" {
		r.URL.Path = matched
		r.RequestURI = matched
	} else if r.URL != nil {
		r.RequestURI = r.URL.String()
	}

	if fiberHandler != nil {
		fiberHandler(w, r)
	} else {
		http.Error(w, "Server initialization failed", http.StatusInternalServerError)
	}
}
