package main

import (
	"log"

	"hr-gateway-backend/config"
	"hr-gateway-backend/controllers"
	"hr-gateway-backend/routes"
	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	app := fiber.New(fiber.Config{ErrorHandler: controllers.ErrorHandler})
	userClient := services.NewSupabaseClient(cfg.SupabaseURL, cfg.SupabaseAnonKey)
	adminClient := services.NewSupabaseAdminClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	mailer := services.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom)
	routes.Register(app, userClient, adminClient, mailer, cfg.InviteBaseURL, cfg.CORSOrigins)
	log.Fatal(app.Listen(":" + cfg.Port))
}
