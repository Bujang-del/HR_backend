package handler

import (
	"log"
	"net/http"
	"strings"
	"sync"

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
	initOnce.Do(initialize)

	targetPath := ""

	// 1. Cek query param jika diteruskan oleh vercel rewrite (?__route_path=...)
	if qPath := r.URL.Query().Get("__route_path"); qPath != "" {
		targetPath = qPath
	}

	// 2. Cek header Vercel X-Matched-Path
	if targetPath == "" {
		if matched := r.Header.Get("X-Matched-Path"); matched != "" && matched != "/api" {
			targetPath = matched
		}
	}

	// 3. Cek header X-Forwarded-Uri
	if targetPath == "" {
		if fwd := r.Header.Get("X-Forwarded-Uri"); fwd != "" {
			parts := strings.Split(fwd, "?")
			targetPath = parts[0]
		}
	}

	// 4. Fallback ke URL.Path atau RequestURI
	if targetPath == "" && r.URL.Path != "" && r.URL.Path != "/api" {
		targetPath = r.URL.Path
	}

	if targetPath != "" {
		if !strings.HasPrefix(targetPath, "/") {
			targetPath = "/" + targetPath
		}
		for strings.HasPrefix(targetPath, "//") {
			targetPath = targetPath[1:]
		}
		r.URL.Path = targetPath
		r.RequestURI = targetPath
	}

	if fiberHandler != nil {
		fiberHandler(w, r)
	} else {
		http.Error(w, "Server initialization failed", http.StatusInternalServerError)
	}
}
