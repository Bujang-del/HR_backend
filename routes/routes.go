package routes

import (
	"strings"

	"hr-gateway-backend/controllers"
	"hr-gateway-backend/middleware"
	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func Register(app *fiber.App, client, adminClient *services.SupabaseClient, mailer *services.SMTPMailer, inviteRedirectURL, corsOrigins string) {
	api := &controllers.API{Supabase: client, SupabaseAdmin: adminClient, Mailer: mailer, InviteRedirectURL: inviteRedirectURL}
	app.Use(cors.New(cors.Config{AllowOrigins: strings.TrimSpace(corsOrigins), AllowHeaders: "Origin, Content-Type, Accept, Authorization"}))
	if inviteRedirectURL != "" {
		app.Get("/", func(ctx *fiber.Ctx) error {
			return ctx.Redirect(inviteRedirectURL, fiber.StatusFound)
		})
	}
	app.Get("/api/v1/health", api.Health)
	app.Post("/api/v1/auth/login", api.Login)
	protected := app.Group("/api/v1", middleware.RequireAuth(client))
	protected.Get("/me", api.Me)
	protected.Patch("/me/profile", api.UpdateMyProfile)
	protected.Get("/shifts/my", api.GetMyShift)
	protected.Get("/biometrics/my-face", api.GetMyBiometricFace)
	protected.Post("/biometrics/face-enroll", api.EnrollMyFace)
	protected.Post("/biometrics/reset-request", api.RequestFaceReset)
	protected.Get("/biometrics/my-reset-request", api.GetMyFaceResetRequest)
	protected.Post("/attendances/clock-in", api.ClockIn)
	protected.Post("/attendances/clock-out", api.ClockOut)
	protected.Get("/attendances/my-today", api.GetTodayAttendance)
	protected.Get("/attendances/my-history", api.GetMyAttendanceHistory)
	protected.Get("/companies/current/shifts", api.ListShifts)
	protected.Post("/companies/current/shifts", api.CreateShift)
	protected.Patch("/companies/current/shifts/:id", api.UpdateShift)
	protected.Delete("/companies/current/shifts/:id", api.DeleteShift)
	protected.Patch("/companies/current/members/:id/shift", api.AssignMemberShift)
	protected.Post("/companies/current", api.CreateCompany)
	protected.Get("/companies/current/departments/:id", api.GetDepartment)
	hr := protected.Group("", middleware.RequireHR(client))
	hr.Get("/companies/current", api.CurrentCompany)
	hr.Get("/companies/current/members", api.ListMembers)
	hr.Post("/companies/current/members", api.AddMember)
	hr.Patch("/companies/current/members/:id", api.UpdateMember)
	hr.Post("/companies/current/members/import", api.ImportMembers)
	hr.Patch("/companies/current/members/:id/department", api.AssignMemberDepartment)
	hr.Get("/companies/current/departments", api.ListDepartments)
	hr.Post("/companies/current/departments", api.CreateDepartment)
	hr.Patch("/companies/current/departments/:id/manager", api.SetDepartmentManager)
	hr.Get("/companies/current/biometrics/requests", api.ListCompanyFaceResetRequests)
	hr.Patch("/companies/current/biometrics/requests/:id", api.ReviewFaceResetRequest)
	hr.Get("/companies/current/attendances", api.ListCompanyAttendances)
	hr.Patch("/companies/current/attendances/:id/review", api.ReviewAttendance)
}


