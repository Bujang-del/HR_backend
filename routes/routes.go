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
	// Pengajuan & Jatah Cuti (Employee / Mobile)
	protected.Get("/leaves/balance", api.GetMyLeaveBalance)
	protected.Post("/leaves/request", api.CreateLeaveRequest)
	protected.Get("/leaves/my", api.GetMyLeaveRequests)

	// Approval Cuti oleh Manager (Manager / Mobile)
	protected.Get("/leaves/manager/pending", api.GetManagerPendingLeaves)
	protected.Patch("/leaves/manager/review/:id", api.ReviewLeaveByManager)

	// Presensi WFA (Work From Anywhere) & Task Board Trello
	protected.Post("/wfa/requests", api.CreateWFARequest)
	protected.Post("/wfa/request", api.CreateWFARequest)
	protected.Post("/wfarequests", api.CreateWFARequest)
	protected.Get("/wfa/my", api.GetMyWFARequests)
	protected.Get("/wfa/today", api.GetTodayWFARequest)
	protected.Patch("/wfa/tasks/:taskId/status", api.UpdateWFATaskStatus)
	protected.Post("/wfa/tasks/:taskId/upload-proof", api.UploadTaskProof)
	protected.Post("/wfa/requests/:id/submit-completion", api.SubmitWFACompletion)

	// Approval WFA oleh Manager
	protected.Get("/wfa/manager/pending", api.GetManagerPendingWFARequests)
	protected.Patch("/wfa/manager/review/:id", api.ReviewWFAByManager)
	protected.Post("/wfa/manager/requests/:id/approve-completion", api.ApproveWFACompletionByManager)

	// Rute Khusus HR (Web Dashboard)
	requireHR := middleware.RequireHR(client)
	protected.Get("/companies/current", requireHR, api.CurrentCompany)
	protected.Get("/companies/current/members", requireHR, api.ListMembers)
	protected.Post("/companies/current/members", requireHR, api.AddMember)
	protected.Patch("/companies/current/members/:id", requireHR, api.UpdateMember)
	protected.Post("/companies/current/members/import", requireHR, api.ImportMembers)
	protected.Patch("/companies/current/members/:id/department", requireHR, api.AssignMemberDepartment)
	protected.Get("/companies/current/departments", requireHR, api.ListDepartments)
	protected.Post("/companies/current/departments", requireHR, api.CreateDepartment)
	protected.Patch("/companies/current/departments/:id/manager", requireHR, api.SetDepartmentManager)
	protected.Get("/companies/current/biometrics/requests", requireHR, api.ListCompanyFaceResetRequests)
	protected.Patch("/companies/current/biometrics/requests/:id", requireHR, api.ReviewFaceResetRequest)
	protected.Get("/companies/current/attendances", requireHR, api.ListCompanyAttendances)
	protected.Patch("/companies/current/attendances/:id/review", requireHR, api.ReviewAttendance)
	protected.Get("/companies/current/leaves", requireHR, api.ListCompanyLeaves)
	protected.Patch("/companies/current/leaves/:id/review", requireHR, api.ReviewLeaveByHR)
	protected.Get("/companies/current/wfa-requests", requireHR, api.ListCompanyWFARequests)
	protected.Patch("/companies/current/wfa-requests/:id/review", requireHR, api.ReviewWFAByHR)
}


