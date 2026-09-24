package routes

import (
	"strings"

	"hr-gateway-backend/controllers"
	"hr-gateway-backend/middleware"
	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func Register(app *fiber.App, client, adminClient *services.SupabaseClient, inviteRedirectURL, corsOrigins string) {
	api := &controllers.API{Supabase: client, SupabaseAdmin: adminClient, InviteRedirectURL: inviteRedirectURL}
	app.Use(cors.New(cors.Config{AllowOrigins: strings.TrimSpace(corsOrigins), AllowHeaders: "Origin, Content-Type, Accept, Authorization"}))
	if inviteRedirectURL != "" {
		app.Get("/", func(ctx *fiber.Ctx) error {
			return ctx.Redirect(inviteRedirectURL, fiber.StatusFound)
		})
	}
	app.Get("/api/v1/health", api.Health)
	protected := app.Group("/api/v1", middleware.RequireAuth(client))
	protected.Get("/me", api.Me)
	protected.Post("/companies/current", api.CreateCompany)
	hr := protected.Group("", middleware.RequireHR(client))
	hr.Get("/companies/current", api.CurrentCompany)
	hr.Get("/companies/current/members", api.ListMembers)
	hr.Post("/companies/current/members", api.AddMember)
	hr.Post("/companies/current/members/import", api.ImportMembers)
	hr.Patch("/companies/current/members/:id/department", api.AssignMemberDepartment)
	hr.Get("/companies/current/departments", api.ListDepartments)
	hr.Post("/companies/current/departments", api.CreateDepartment)
	hr.Get("/companies/current/departments/:id", api.GetDepartment)
	hr.Patch("/companies/current/departments/:id/manager", api.SetDepartmentManager)
}
