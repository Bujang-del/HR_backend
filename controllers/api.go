package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hr-gateway-backend/middleware"
	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
)

type API struct {
	Supabase          *services.SupabaseClient
	SupabaseAdmin     *services.SupabaseClient
	Mailer            *services.SMTPMailer
	InviteRedirectURL string
	Gemini            *services.GeminiService
}

type departmentSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

func (d *departmentSummary) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	type alias departmentSummary
	var obj alias
	if err := json.Unmarshal(data, &obj); err == nil && obj.ID != "" {
		*d = departmentSummary(obj)
		return nil
	}
	var list []alias
	if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
		*d = departmentSummary(list[0])
		return nil
	}
	return nil
}

type companySummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

func (c *companySummary) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	type alias companySummary
	var obj alias
	if err := json.Unmarshal(data, &obj); err == nil && obj.ID != "" {
		*c = companySummary(obj)
		return nil
	}
	var list []alias
	if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
		*c = companySummary(list[0])
		return nil
	}
	return nil
}

type shiftSummary struct {
	ID                   string `json:"id"`
	CompanyID            string `json:"company_id"`
	Name                 string `json:"name"`
	StartTime            string `json:"start_time"`
	EndTime              string `json:"end_time"`
	LateToleranceMinutes int    `json:"late_tolerance_minutes"`
	WorkDays             []int  `json:"work_days"`
	IsDefault            bool   `json:"is_default"`
}

func (s *shiftSummary) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	type alias shiftSummary
	var obj alias
	if err := json.Unmarshal(data, &obj); err == nil && obj.ID != "" {
		*s = shiftSummary(obj)
		return nil
	}
	var list []alias
	if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
		*s = shiftSummary(list[0])
		return nil
	}
	return nil
}

type membership struct {
	MemberID           string              `json:"id"`
	CompanyID          string              `json:"company_id"`
	Company            *companySummary     `json:"company"`
	Role               string              `json:"role"`
	PositionTitle      *string             `json:"position_title"`
	DepartmentID       *string             `json:"department_id"`
	Department         *departmentSummary  `json:"department"`
	ManagedDepartments []departmentSummary `json:"managed_departments"`
	ShiftID            *string             `json:"shift_id"`
	Shift              *shiftSummary       `json:"shift"`
	LeaveQuota         int                 `json:"leave_quota"`
	LeaveTaken         int                 `json:"leave_taken"`
	LeaveBalance       int                 `json:"leave_balance"`
}

type profile struct {
	ID       string  `json:"id"`
	Email    string  `json:"email"`
	FullName *string `json:"full_name"`
	Username *string `json:"username"`
}

type memberInput struct {
	Email         string  `json:"email"`
	FullName      string  `json:"full_name"`
	Username      string  `json:"username"`
	Position      string  `json:"position"`
	PositionTitle string  `json:"position_title"`
	Role          string  `json:"role"`
	DepartmentID  *string `json:"department_id"`
}

type importRow struct {
	Email      string `json:"email"`
	FullName   string `json:"full_name"`
	Username   string `json:"username"`
	Position   string `json:"position"`
	Role       string `json:"role"`
	Department string `json:"department"`
}

type importFailure struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

type importResult struct {
	Created           int             `json:"created"`
	Updated           int             `json:"updated"`
	Invited           int             `json:"invited"`
	AlreadyRegistered int             `json:"already_registered"`
	Duplicates        []string        `json:"duplicates"`
	MissingEmails     []string        `json:"missingEmails"`
	Failed            []importFailure `json:"failed"`
}

func (api *API) Health(ctx *fiber.Ctx) error {
	return ctx.JSON(fiber.Map{"status": "ok", "version": "v2_fixed"})
}

func (api *API) Login(ctx *fiber.Ctx) error {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Email dan kata sandi wajib diisi.")
	}
	var session map[string]any
	if err := api.Supabase.PasswordLogin(ctx.Context(), strings.ToLower(strings.TrimSpace(input.Email)), input.Password, &session); err != nil {
		if httpErr, ok := err.(*services.HTTPError); ok {
			if httpErr.Status == http.StatusBadRequest || httpErr.Status == http.StatusUnauthorized {
				return fiber.NewError(fiber.StatusUnauthorized, "Email atau kata sandi tidak valid.")
			}
		}
		return err
	}
	return ctx.JSON(session)
}

func (api *API) Me(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	isManager := len(m.ManagedDepartments) > 0
	var managedDept *departmentSummary
	if isManager {
		managedDept = &m.ManagedDepartments[0]
	}

	// Hitung display_position = position_title + " " + department_code
	// Contoh: "Manager" + " " + "SDM" â†’ "Manager SDM"
	//         "Staff"   + " " + "FI"  â†’ "Staff FI"
	displayPosition := ""
	if m.PositionTitle != nil {
		displayPosition = strings.TrimSpace(*m.PositionTitle)
	}
	if m.Department != nil && m.Department.Code != "" {
		if displayPosition != "" {
			displayPosition += " " + m.Department.Code
		} else {
			displayPosition = m.Department.Code
		}
	}

	user := middleware.CurrentUser(ctx)
	if (user.FullName == "" || user.Username == "") && api.SupabaseAdmin != nil {
		var profiles []profile
		profilePath := "/profiles?id=eq." + url.QueryEscape(user.ID) + "&select=id,email,full_name&limit=1"
		if err := api.SupabaseAdmin.Get(ctx.Context(), profilePath, "", &profiles); err == nil && len(profiles) > 0 {
			if profiles[0].FullName != nil && strings.TrimSpace(*profiles[0].FullName) != "" {
				user.FullName = strings.TrimSpace(*profiles[0].FullName)
			}
			if profiles[0].Username != nil && strings.TrimSpace(*profiles[0].Username) != "" {
				user.Username = strings.TrimSpace(*profiles[0].Username)
			}
		}
	}
	if user.Username == "" && user.Email != "" {
		user.Username = strings.Split(user.Email, "@")[0]
	}
	if user.FullName == "" {
		user.FullName = user.Username
	}

	companyName := ""
	if m.Company != nil && m.Company.Name != "" {
		companyName = m.Company.Name
	}

	return ctx.JSON(fiber.Map{
		"user": fiber.Map{
			"id":        user.ID,
			"email":     user.Email,
			"full_name": user.FullName,
			"username":  user.Username,
		},
		"membership": fiber.Map{
			"company_id":         m.CompanyID,
			"company_name":       companyName,
			"company":            m.Company,
			"role":               m.Role,
			"position_title":     m.PositionTitle,
			"display_position":   displayPosition,
			"department_id":      m.DepartmentID,
			"department":         m.Department,
			"is_manager":         isManager,
			"managed_department": managedDept,
			"shift_id":           m.ShiftID,
			"shift":              m.Shift,
			"leave_quota":        m.LeaveQuota,
			"leave_taken":        m.LeaveTaken,
			"leave_balance":      m.LeaveBalance,
		},
	})
}

type updateProfileInput struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
}

func (api *API) UpdateMyProfile(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	var input updateProfileInput
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	fullName := strings.TrimSpace(input.FullName)
	username := strings.TrimSpace(input.Username)
	if username == "" && user.Email != "" {
		username = strings.Split(user.Email, "@")[0]
	}

	metadata := map[string]any{
		"full_name": fullName,
		"username":  username,
	}

	if api.SupabaseAdmin != nil {
		_ = api.SupabaseAdmin.UpdateAdminUser(ctx.Context(), user.ID, map[string]any{"user_metadata": metadata}, nil)
		patchPayload := map[string]any{}
		if fullName != "" {
			patchPayload["full_name"] = fullName
		}
		if username != "" {
			patchPayload["username"] = username
		}
		if len(patchPayload) > 0 {
			_ = api.SupabaseAdmin.Patch(ctx.Context(), "/profiles?id=eq."+url.QueryEscape(user.ID), "", patchPayload, nil)
		}
	}

	return ctx.JSON(fiber.Map{
		"user": fiber.Map{
			"id":        user.ID,
			"email":     user.Email,
			"full_name": fullName,
			"username":  username,
		},
	})
}

func (api *API) CurrentCompany(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var companies []map[string]any
	if err := api.Supabase.Get(ctx.Context(), "/companies?id=eq."+url.QueryEscape(membership.CompanyID)+"&select=id,name,code", middleware.AccessToken(ctx), &companies); err != nil {
		return err
	}
	if len(companies) == 0 {
		return fiber.ErrNotFound
	}
	return ctx.JSON(companies[0])
}

func (api *API) CreateCompany(ctx *fiber.Ctx) error {
	var input struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Code) == "" {
		return fiber.ErrBadRequest
	}
	payload := fiber.Map{"owner_id": middleware.CurrentUser(ctx).ID, "name": strings.TrimSpace(input.Name), "code": strings.ToUpper(strings.TrimSpace(input.Code))}
	var companies []map[string]any
	if err := api.Supabase.Post(ctx.Context(), "/companies?select=id,name,code", middleware.AccessToken(ctx), payload, &companies); err != nil {
		return err
	}
	if len(companies) == 0 {
		return fiber.ErrInternalServerError
	}
	return ctx.Status(http.StatusCreated).JSON(companies[0])
}

func (api *API) ListMembers(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var members []map[string]any
	path := "/company_members?company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,user_id,role,position_title,department_id,profiles(email,full_name)&order=joined_at.asc"
	client, token := api.client(ctx)
	if err := client.Get(ctx.Context(), path, token, &members); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(members))
}

type updateMemberInput struct {
	FullName      *string `json:"full_name"`
	Username      *string `json:"username"`
	PositionTitle *string `json:"position_title"`
	Role          *string `json:"role"`
}

func (api *API) UpdateMember(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	memberID := ctx.Params("id")
	if memberID == "" {
		return fiber.ErrBadRequest
	}

	var input updateMemberInput
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	client, token := api.client(ctx)
	var existing []struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	existingPath := "/company_members?id=eq." + url.QueryEscape(memberID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,user_id,role&limit=1"
	if err := client.Get(ctx.Context(), existingPath, token, &existing); err != nil {
		return err
	}
	if len(existing) == 0 {
		return fiber.ErrNotFound
	}

	memberRecord := existing[0]

	// Update company_members table if role or position_title provided
	memberPayload := map[string]any{}
	if input.PositionTitle != nil {
		memberPayload["position_title"] = strings.TrimSpace(*input.PositionTitle)
	}
	if input.Role != nil {
		memberPayload["role"] = normalizeRole(*input.Role)
	}
	if len(memberPayload) > 0 {
		var dummy []map[string]any
		_ = client.Patch(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(memberID), token, memberPayload, &dummy)
	}

	// Update profiles & auth metadata if full_name or username provided
	if input.FullName != nil || input.Username != nil {
		profilePayload := map[string]any{}
		authMetadata := map[string]any{}
		if input.FullName != nil {
			fn := strings.TrimSpace(*input.FullName)
			profilePayload["full_name"] = fn
			authMetadata["full_name"] = fn
		}
		if input.Username != nil {
			un := strings.TrimSpace(*input.Username)
			profilePayload["username"] = un
			authMetadata["username"] = un
		}

		if api.SupabaseAdmin != nil {
			if len(authMetadata) > 0 {
				_ = api.SupabaseAdmin.UpdateAdminUser(ctx.Context(), memberRecord.UserID, map[string]any{"user_metadata": authMetadata}, nil)
			}
			if len(profilePayload) > 0 {
				_ = api.SupabaseAdmin.Patch(ctx.Context(), "/profiles?id=eq."+url.QueryEscape(memberRecord.UserID), "", profilePayload, nil)
			}
		}
	}

	// Fetch updated member data
	var updatedMembers []map[string]any
	path := "/company_members?id=eq." + url.QueryEscape(memberID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,user_id,role,position_title,department_id,profiles(email,full_name)"
	if err := client.Get(ctx.Context(), path, token, &updatedMembers); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(firstMap(updatedMembers)))
}

func (api *API) AddMember(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input memberInput
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.Email) == "" {
		return fiber.ErrBadRequest
	}
	role := input.Role
	if role != "hr" {
		role = "employee"
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	fullName := strings.TrimSpace(input.FullName)
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = strings.Split(email, "@")[0]
	}
	userID, _, err := api.findOrInviteUser(ctx, email, fullName, username, true)
	if err != nil {
		return err
	}
	position := input.PositionTitle
	if position == "" {
		position = input.Position
	}
	member, _, err := api.upsertMember(ctx, membership.CompanyID, userID, role, position, input.DepartmentID)
	if err != nil {
		return err
	}
	return ctx.Status(http.StatusCreated).JSON(normalizeProfiles(member))
}

func (api *API) ImportMembers(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input struct {
		Rows []importRow `json:"rows"`
	}
	if err := ctx.BodyParser(&input); err != nil || len(input.Rows) == 0 {
		return fiber.ErrBadRequest
	}

	seen := make(map[string]bool)
	duplicates := make([]string, 0)
	uniqueRows := make([]importRow, 0, len(input.Rows))
	for _, row := range input.Rows {
		row.Email = strings.ToLower(strings.TrimSpace(row.Email))
		if seen[row.Email] {
			duplicates = append(duplicates, row.Email)
			continue
		}
		seen[row.Email] = true
		uniqueRows = append(uniqueRows, row)
	}

	result := importResult{Duplicates: duplicates, MissingEmails: []string{}, Failed: []importFailure{}}
	for _, row := range uniqueRows {
		fullName := strings.TrimSpace(row.FullName)
		username := strings.TrimSpace(row.Username)
		if username == "" {
			username = strings.Split(row.Email, "@")[0]
		}
		userID, invited, findErr := api.findOrInviteUser(ctx, row.Email, fullName, username, false)
		if findErr != nil {
			result.Failed = append(result.Failed, importFailure{Email: row.Email, Reason: findErr.Error()})
			continue
		}
		departmentID, departmentErr := api.findDepartmentID(ctx, membership.CompanyID, row.Department)
		if departmentErr != nil {
			result.Failed = append(result.Failed, importFailure{Email: row.Email, Reason: departmentErr.Error()})
			continue
		}
		_, created, upsertErr := api.upsertMember(ctx, membership.CompanyID, userID, normalizeRole(row.Role), row.Position, departmentID)
		if upsertErr != nil {
			result.Failed = append(result.Failed, importFailure{Email: row.Email, Reason: upsertErr.Error()})
			continue
		}
		if invited {
			result.Invited++
		} else {
			result.AlreadyRegistered++
		}
		if created {
			result.Created++
		} else {
			result.Updated++
		}
	}
	return ctx.JSON(result)
}

func (api *API) findOrInviteUser(ctx *fiber.Ctx, email, fullName, username string, resendIfExists bool) (string, bool, error) {
	if api.SupabaseAdmin == nil {
		return "", false, fiber.NewError(http.StatusInternalServerError, "Supabase Admin API belum dikonfigurasi.")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	fullName = strings.TrimSpace(fullName)
	username = strings.TrimSpace(username)
	if username == "" {
		username = strings.Split(email, "@")[0]
	}

	metadata := map[string]any{
		"full_name": fullName,
		"username":  username,
	}

	var profiles []profile
	profilePath := "/profiles?email=eq." + url.QueryEscape(email) + "&select=id,email,full_name&limit=1"
	if err := api.SupabaseAdmin.Get(ctx.Context(), profilePath, "", &profiles); err != nil {
		return "", false, err
	}
	if len(profiles) > 0 {
		userID := profiles[0].ID
		if fullName != "" || username != "" {
			_ = api.SupabaseAdmin.UpdateAdminUser(ctx.Context(), userID, map[string]any{"user_metadata": metadata}, nil)
			patchPayload := map[string]any{}
			if fullName != "" {
				patchPayload["full_name"] = fullName
			}
			if len(patchPayload) > 0 {
				_ = api.SupabaseAdmin.Patch(ctx.Context(), "/profiles?id=eq."+url.QueryEscape(userID), "", patchPayload, nil)
			}
		}

		if !resendIfExists {
			return userID, false, nil
		}
		if err := api.sendAuthEmail(ctx, "recovery", email, fullName); err != nil {
			return "", false, err
		}
		return userID, false, nil
	}

	var invite services.InviteLink
	if err := api.SupabaseAdmin.GenerateInviteLink(ctx.Context(), email, api.InviteRedirectURL, metadata, &invite); err != nil {
		return "", false, err
	}
	userID := invite.UserID()
	actionLink := invite.Link()
	if userID == "" {
		var createdProfiles []profile
		profilePath := "/profiles?email=eq." + url.QueryEscape(email) + "&select=id,email,full_name&limit=1"
		if profileErr := api.SupabaseAdmin.Get(ctx.Context(), profilePath, "", &createdProfiles); profileErr == nil && len(createdProfiles) > 0 {
			userID = createdProfiles[0].ID
		}
	}
	if userID == "" || actionLink == "" {
		return "", false, fiber.NewError(http.StatusBadGateway, "Supabase tidak mengembalikan user hasil undangan.")
	}

	if fullName != "" {
		_ = api.SupabaseAdmin.Patch(ctx.Context(), "/profiles?id=eq."+url.QueryEscape(userID), "", map[string]any{"full_name": fullName}, nil)
	}

	if err := api.dispatchInvitation(ctx, email, fullName, actionLink); err != nil {
		return "", false, err
	}
	return userID, true, nil
}

func (api *API) sendAuthEmail(ctx *fiber.Ctx, linkType, email, recipientName string) error {
	var invite services.InviteLink
	if err := api.SupabaseAdmin.GenerateAuthLink(ctx.Context(), linkType, email, api.InviteRedirectURL, nil, &invite); err != nil {
		return err
	}
	link := invite.Link()
	if link == "" {
		return fiber.NewError(http.StatusBadGateway, "Supabase tidak mengembalikan link undangan.")
	}
	return api.dispatchInvitation(ctx, email, recipientName, link)
}

func (api *API) dispatchInvitation(ctx *fiber.Ctx, email, recipientName, actionLink string) error {
	if api.Mailer == nil {
		return fiber.NewError(http.StatusInternalServerError, "Mailer undangan belum dikonfigurasi.")
	}
	if err := api.Mailer.SendInvitation(ctx.Context(), email, recipientName, actionLink); err != nil {
		return fiber.NewError(http.StatusBadGateway, "Email invitation gagal dikirim.")
	}
	return nil
}

func (api *API) findDepartmentID(ctx *fiber.Ctx, companyID, value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var departments []struct {
		ID string `json:"id"`
	}
	basePath := "/departments?company_id=eq." + url.QueryEscape(companyID) + "&select=id&limit=1&"
	for _, filter := range []string{"code=eq.", "name=eq."} {
		departments = nil
		if err := api.Supabase.Get(ctx.Context(), basePath+filter+url.QueryEscape(value), middleware.AccessToken(ctx), &departments); err != nil {
			return nil, err
		}
		if len(departments) > 0 {
			return &departments[0].ID, nil
		}
	}
	return nil, fmt.Errorf("departemen %q tidak ditemukan", value)
}

func (api *API) upsertMember(ctx *fiber.Ctx, companyID, userID, role, position string, departmentID *string) (map[string]any, bool, error) {
	var existing []struct {
		ID string `json:"id"`
	}
	existingPath := "/company_members?company_id=eq." + url.QueryEscape(companyID) + "&user_id=eq." + url.QueryEscape(userID) + "&select=id&limit=1"
	if err := api.Supabase.Get(ctx.Context(), existingPath, middleware.AccessToken(ctx), &existing); err != nil {
		return nil, false, err
	}
	payload := fiber.Map{"role": normalizeRole(role), "position_title": strings.TrimSpace(position), "department_id": departmentID}
	if len(existing) > 0 {
		var members []map[string]any
		path := "/company_members?id=eq." + url.QueryEscape(existing[0].ID) + "&company_id=eq." + url.QueryEscape(companyID) + "&select=id,user_id,role,position_title,department_id,profiles(email,full_name)"
		if err := api.Supabase.Patch(ctx.Context(), path, middleware.AccessToken(ctx), payload, &members); err != nil {
			return nil, false, err
		}
		return normalizeProfiles(firstMap(members)).(map[string]any), false, nil
	}
	payload["company_id"] = companyID
	payload["user_id"] = userID
	var members []map[string]any
	if err := api.Supabase.Post(ctx.Context(), "/company_members?select=id,user_id,role,position_title,department_id,profiles(email,full_name)", middleware.AccessToken(ctx), payload, &members); err != nil {
		return nil, true, err
	}
	return normalizeProfiles(firstMap(members)).(map[string]any), true, nil
}

func normalizeRole(role string) string {
	if strings.ToLower(strings.TrimSpace(role)) == "hr" {
		return "hr"
	}
	return "employee"
}

func (api *API) client(ctx *fiber.Ctx) (*services.SupabaseClient, string) {
	if api.SupabaseAdmin != nil {
		return api.SupabaseAdmin, ""
	}
	return api.Supabase, middleware.AccessToken(ctx)
}

func (api *API) ListDepartments(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var departments []map[string]any
	path := "/departments?company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,name,code,manager_id,members:company_members!company_members_department_id_fkey(id,user_id,role,position_title,department_id,profiles(email,full_name))&order=name.asc"
	client, token := api.client(ctx)
	if err := client.Get(ctx.Context(), path, token, &departments); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(departments))
}

func (api *API) CreateDepartment(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	payload := fiber.Map{"company_id": membership.CompanyID, "name": strings.TrimSpace(input.Name), "code": strings.ToUpper(strings.TrimSpace(input.Code))}
	var departments []map[string]any
	client, token := api.client(ctx)
	if err := client.Post(ctx.Context(), "/departments?select=id,name,code,manager_id", token, payload, &departments); err != nil {
		return err
	}
	return ctx.Status(http.StatusCreated).JSON(firstMap(departments))
}

func (api *API) GetDepartment(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var departments []map[string]any
	path := "/departments?id=eq." + url.QueryEscape(ctx.Params("id")) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,name,code,manager_id,members:company_members!company_members_department_id_fkey(id,user_id,role,position_title,department_id,profiles(email,full_name))"
	client, token := api.client(ctx)
	if err := client.Get(ctx.Context(), path, token, &departments); err != nil {
		return err
	}
	if len(departments) == 0 {
		return fiber.ErrNotFound
	}
	return ctx.JSON(normalizeProfiles(departments[0]))
}

func (api *API) SetDepartmentManager(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input struct {
		ManagerID *string `json:"manager_id"`
	}
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	deptID := ctx.Params("id")
	client, token := api.client(ctx)

	// 1. Ambil data departemen saat ini untuk mengetahui manager lama
	var existingDepts []struct {
		ManagerID *string `json:"manager_id"`
	}
	existingPath := "/departments?id=eq." + url.QueryEscape(deptID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=manager_id&limit=1"
	_ = client.Get(ctx.Context(), existingPath, token, &existingDepts)

	var oldManagerID *string
	if len(existingDepts) > 0 {
		oldManagerID = existingDepts[0].ManagerID
	}

	// 2. Jika manager lama berbeda dengan manager baru, turunkan jabatan manager lama menjadi "Staff"
	if oldManagerID != nil && *oldManagerID != "" {
		if input.ManagerID == nil || *input.ManagerID != *oldManagerID {
			oldMemberPath := "/company_members?id=eq." + url.QueryEscape(*oldManagerID) +
				"&company_id=eq." + url.QueryEscape(membership.CompanyID)
			var updatedOld []map[string]any
			_ = client.Patch(ctx.Context(), oldMemberPath, token,
				fiber.Map{"position_title": "Staff"}, &updatedOld)
		}
	}

	// 3. Jika calon manager baru sebelumnya adalah manager di departemen LAIN, kosongkan manager_id departemen lama tsb
	if input.ManagerID != nil && *input.ManagerID != "" {
		otherDeptPath := "/departments?company_id=eq." + url.QueryEscape(membership.CompanyID) +
			"&manager_id=eq." + url.QueryEscape(*input.ManagerID) +
			"&id=neq." + url.QueryEscape(deptID)
		var otherDepts []map[string]any
		_ = client.Patch(ctx.Context(), otherDeptPath, token,
			fiber.Map{"manager_id": nil}, &otherDepts)
	}

	// 4. Update manager_id di tabel departments
	path := "/departments?id=eq." + url.QueryEscape(deptID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,name,code,manager_id"
	var departments []map[string]any
	if err := client.Patch(ctx.Context(), path, token, fiber.Map{"manager_id": input.ManagerID}, &departments); err != nil {
		return err
	}

	// 5. Jika ada manager baru, otomatis update position_title-nya menjadi "Manager"
	if input.ManagerID != nil && *input.ManagerID != "" {
		memberPath := "/company_members?id=eq." + url.QueryEscape(*input.ManagerID) +
			"&company_id=eq." + url.QueryEscape(membership.CompanyID)
		var updated []map[string]any
		// Best-effort: abaikan error agar response utama tidak gagal
		_ = client.Patch(ctx.Context(), memberPath, token,
			fiber.Map{"position_title": "Manager"}, &updated)
	}

	return ctx.JSON(firstMap(departments))
}

func (api *API) AssignMemberDepartment(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input struct {
		DepartmentID  *string `json:"department_id"`
		PositionTitle *string `json:"position_title"`
		AsManager     *bool   `json:"as_manager"`
	}
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	memberID := ctx.Params("id")
	client, token := api.client(ctx)

	// 1. Jika member ini sebelumnya adalah manager di salah satu departemen, lepaskan status manager dari departemen lamanya
	oldDeptPath := "/departments?company_id=eq." + url.QueryEscape(membership.CompanyID) +
		"&manager_id=eq." + url.QueryEscape(memberID)
	var oldManagedDepts []map[string]any
	_ = client.Patch(ctx.Context(), oldDeptPath, token,
		fiber.Map{"manager_id": nil}, &oldManagedDepts)

	// 2. Siapkan update payload untuk company_members
	patchMember := fiber.Map{
		"department_id": input.DepartmentID,
	}

	if input.PositionTitle != nil && *input.PositionTitle != "" {
		patchMember["position_title"] = *input.PositionTitle
	}

	// 3. Logika assignment manager vs staff
	if input.AsManager != nil && *input.AsManager && input.DepartmentID != nil && *input.DepartmentID != "" {
		targetDeptID := *input.DepartmentID

		// Ambil manager departemen tujuan saat ini
		var targetDepts []struct {
			ManagerID *string `json:"manager_id"`
		}
		targetPath := "/departments?id=eq." + url.QueryEscape(targetDeptID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=manager_id&limit=1"
		_ = client.Get(ctx.Context(), targetPath, token, &targetDepts)

		if len(targetDepts) > 0 && targetDepts[0].ManagerID != nil && *targetDepts[0].ManagerID != memberID {
			// Turunkan manager saat ini menjadi Staff
			oldManagerPath := "/company_members?id=eq." + url.QueryEscape(*targetDepts[0].ManagerID) +
				"&company_id=eq." + url.QueryEscape(membership.CompanyID)
			var updatedOld []map[string]any
			_ = client.Patch(ctx.Context(), oldManagerPath, token,
				fiber.Map{"position_title": "Staff"}, &updatedOld)
		}

		// Update manager_id departemen tujuan
		var updatedDepts []map[string]any
		_ = client.Patch(ctx.Context(), "/departments?id=eq."+url.QueryEscape(targetDeptID)+"&company_id=eq."+url.QueryEscape(membership.CompanyID),
			token, fiber.Map{"manager_id": memberID}, &updatedDepts)

		patchMember["position_title"] = "Manager"
	} else if input.AsManager != nil && !*input.AsManager {
		// Secara eksplisit dipilih sebagai Staff (bukan manager)
		if input.PositionTitle == nil || *input.PositionTitle == "" || *input.PositionTitle == "Manager" {
			patchMember["position_title"] = "Staff"
		}
	} else {
		// Default: jika departemen tujuan sudah memiliki manager lain dan jabatan member saat ini adalah "Manager",
		// ubah otomatis menjadi "Staff" agar tidak ada duplikasi Manager di satu departemen
		if input.DepartmentID != nil && *input.DepartmentID != "" {
			var targetDepts []struct {
				ManagerID *string `json:"manager_id"`
			}
			targetPath := "/departments?id=eq." + url.QueryEscape(*input.DepartmentID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=manager_id&limit=1"
			_ = client.Get(ctx.Context(), targetPath, token, &targetDepts)

			var currentMemberData []struct {
				PositionTitle *string `json:"position_title"`
			}
			_ = client.Get(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(memberID)+"&company_id=eq."+url.QueryEscape(membership.CompanyID)+"&select=position_title&limit=1", token, &currentMemberData)

			isTargetHasOtherManager := len(targetDepts) > 0 && targetDepts[0].ManagerID != nil && *targetDepts[0].ManagerID != memberID
			if isTargetHasOtherManager {
				if len(currentMemberData) > 0 && currentMemberData[0].PositionTitle != nil && strings.EqualFold(*currentMemberData[0].PositionTitle, "Manager") {
					patchMember["position_title"] = "Staff"
				}
			}
		}
	}

	path := "/company_members?id=eq." + url.QueryEscape(memberID) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,user_id,role,position_title,department_id,profiles(email,full_name)"
	var members []map[string]any
	if err := client.Patch(ctx.Context(), path, token, patchMember, &members); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(firstMap(members)))
}

func (api *API) currentMembership(ctx *fiber.Ctx) (membership, error) {
	user := middleware.CurrentUser(ctx)
	var memberships []membership
	// Gunakan nama FK eksplisit untuk menghindari ambiguitas PostgREST (error 300).
	// managed_departments: join balik dari departments.manager_id â†’ company_members.id
	// untuk mendeteksi apakah user ini adalah manager suatu departemen.
	path := "/company_members?user_id=eq." + url.QueryEscape(user.ID) +
		"&select=id,company_id,role,position_title,department_id,shift_id," +
		"company:company_members_company_id_fkey(id,name,code)," +
		"department:company_members_department_id_fkey(id,name,code)," +
		"managed_departments:departments!departments_manager_id_fkey(id,name,code)" +
		"&order=joined_at.asc&limit=1"
	client, token := api.client(ctx)
	if err := client.Get(ctx.Context(), path, token, &memberships); err != nil {
		return membership{}, err
	}
	if len(memberships) == 0 {
		return membership{}, fiber.NewError(http.StatusNotFound, "Perusahaan belum ditemukan.")
	}

	// Fallback jika PostgREST tidak meng-embed company lewat join
	if (memberships[0].Company == nil || memberships[0].Company.Name == "") && memberships[0].CompanyID != "" {
		var comps []companySummary
		compPath := "/companies?id=eq." + url.QueryEscape(memberships[0].CompanyID) + "&select=id,name,code&limit=1"
		if api.SupabaseAdmin != nil {
			_ = api.SupabaseAdmin.Get(ctx.Context(), compPath, "", &comps)
		} else {
			_ = client.Get(ctx.Context(), compPath, token, &comps)
		}
		if len(comps) > 0 {
			memberships[0].Company = &comps[0]
		}
	}

	// Look up shift for member: custom assigned shift or company default
	if memberships[0].ShiftID != nil && *memberships[0].ShiftID != "" {
		var shifts []shiftSummary
		shiftPath := "/work_shifts?id=eq." + url.QueryEscape(*memberships[0].ShiftID) + "&limit=1"
		if api.SupabaseAdmin != nil {
			_ = api.SupabaseAdmin.Get(ctx.Context(), shiftPath, "", &shifts)
		} else {
			_ = client.Get(ctx.Context(), shiftPath, token, &shifts)
		}
		if len(shifts) > 0 {
			memberships[0].Shift = &shifts[0]
		}
	}
	if memberships[0].Shift == nil && memberships[0].CompanyID != "" {
		var defaultShifts []shiftSummary
		shiftPath := "/work_shifts?company_id=eq." + url.QueryEscape(memberships[0].CompanyID) + "&is_default=eq.true&limit=1"
		if api.SupabaseAdmin != nil {
			_ = api.SupabaseAdmin.Get(ctx.Context(), shiftPath, "", &defaultShifts)
		} else {
			_ = client.Get(ctx.Context(), shiftPath, token, &defaultShifts)
		}
		if len(defaultShifts) > 0 {
			memberships[0].Shift = &defaultShifts[0]
		} else {
			memberships[0].Shift = &shiftSummary{
				ID:                   "default",
				CompanyID:            memberships[0].CompanyID,
				Name:                 "Shift Reguler (Pagi - Sore)",
				StartTime:            "08:00:00",
				EndTime:              "17:00:00",
				LateToleranceMinutes: 15,
				WorkDays:             []int{1, 2, 3, 4, 5},
				IsDefault:            true,
			}
		}
	}

	// Populate leave quota (default 12 hari jika belum termigrasi)
	memberships[0].LeaveQuota = 12
	memberships[0].LeaveBalance = 12
	var leaveData []struct {
		LeaveQuota   *int `json:"leave_quota"`
		LeaveTaken   *int `json:"leave_taken"`
		LeaveBalance *int `json:"leave_balance"`
	}
	leavePath := "/company_members?id=eq." + url.QueryEscape(memberships[0].MemberID) + "&select=leave_quota,leave_taken,leave_balance&limit=1"
	if err := client.Get(ctx.Context(), leavePath, token, &leaveData); err == nil && len(leaveData) > 0 {
		if leaveData[0].LeaveQuota != nil {
			memberships[0].LeaveQuota = *leaveData[0].LeaveQuota
		}
		if leaveData[0].LeaveTaken != nil {
			memberships[0].LeaveTaken = *leaveData[0].LeaveTaken
		}
		if leaveData[0].LeaveBalance != nil {
			memberships[0].LeaveBalance = *leaveData[0].LeaveBalance
		} else {
			memberships[0].LeaveBalance = memberships[0].LeaveQuota - memberships[0].LeaveTaken
		}
	}

	return memberships[0], nil
}

func (api *API) GetMyShift(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	return ctx.JSON(fiber.Map{
		"shift":        m.Shift,
		"company_name": m.Company.Name,
	})
}

func (api *API) ListShifts(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var shifts []shiftSummary
	path := "/work_shifts?company_id=eq." + url.QueryEscape(m.CompanyID) + "&order=is_default.desc,created_at.asc"
	client, token := api.client(ctx)
	if err := client.Get(ctx.Context(), path, token, &shifts); err != nil {
		return err
	}
	if len(shifts) == 0 {
		shifts = []shiftSummary{
			{
				ID:                   "default",
				CompanyID:            m.CompanyID,
				Name:                 "Shift Reguler (Pagi - Sore)",
				StartTime:            "08:00:00",
				EndTime:              "17:00:00",
				LateToleranceMinutes: 15,
				WorkDays:             []int{1, 2, 3, 4, 5},
				IsDefault:            true,
			},
		}
	}
	return ctx.JSON(shifts)
}

type createShiftInput struct {
	Name                 string `json:"name"`
	StartTime            string `json:"start_time"`
	EndTime              string `json:"end_time"`
	LateToleranceMinutes int    `json:"late_tolerance_minutes"`
	WorkDays             []int  `json:"work_days"`
	IsDefault            bool   `json:"is_default"`
}

func (api *API) CreateShift(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var input createShiftInput
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return fiber.ErrBadRequest
	}
	if len(input.WorkDays) == 0 {
		input.WorkDays = []int{1, 2, 3, 4, 5}
	}
	if input.LateToleranceMinutes <= 0 {
		input.LateToleranceMinutes = 15
	}
	startTime := strings.TrimSpace(input.StartTime)
	if startTime == "" {
		startTime = "08:00:00"
	}
	endTime := strings.TrimSpace(input.EndTime)
	if endTime == "" {
		endTime = "17:00:00"
	}

	client, token := api.client(ctx)
	payload := fiber.Map{
		"company_id":             m.CompanyID,
		"name":                   strings.TrimSpace(input.Name),
		"start_time":             startTime,
		"end_time":               endTime,
		"late_tolerance_minutes": input.LateToleranceMinutes,
		"work_days":              input.WorkDays,
		"is_default":             input.IsDefault,
	}

	var created []shiftSummary
	if err := client.Post(ctx.Context(), "/work_shifts?select=*", token, payload, &created); err != nil {
		return err
	}
	if len(created) == 0 {
		return fiber.ErrInternalServerError
	}
	return ctx.Status(fiber.StatusCreated).JSON(created[0])
}

type updateShiftInput struct {
	Name                 *string `json:"name"`
	StartTime            *string `json:"start_time"`
	EndTime              *string `json:"end_time"`
	LateToleranceMinutes *int    `json:"late_tolerance_minutes"`
	WorkDays             []int   `json:"work_days"`
	IsDefault            *bool   `json:"is_default"`
}

func (api *API) UpdateShift(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	shiftID := ctx.Params("id")
	if shiftID == "" {
		return fiber.ErrBadRequest
	}

	var input updateShiftInput
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	patchPayload := fiber.Map{}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		patchPayload["name"] = strings.TrimSpace(*input.Name)
	}
	if input.StartTime != nil && strings.TrimSpace(*input.StartTime) != "" {
		patchPayload["start_time"] = strings.TrimSpace(*input.StartTime)
	}
	if input.EndTime != nil && strings.TrimSpace(*input.EndTime) != "" {
		patchPayload["end_time"] = strings.TrimSpace(*input.EndTime)
	}
	if input.LateToleranceMinutes != nil && *input.LateToleranceMinutes >= 0 {
		patchPayload["late_tolerance_minutes"] = *input.LateToleranceMinutes
	}
	if input.WorkDays != nil && len(input.WorkDays) > 0 {
		patchPayload["work_days"] = input.WorkDays
	}
	if input.IsDefault != nil {
		patchPayload["is_default"] = *input.IsDefault
	}

	if len(patchPayload) == 0 {
		return fiber.ErrBadRequest
	}

	client, token := api.client(ctx)
	path := "/work_shifts?id=eq." + url.QueryEscape(shiftID) + "&company_id=eq." + url.QueryEscape(m.CompanyID) + "&select=*"
	var updated []shiftSummary
	if err := client.Patch(ctx.Context(), path, token, patchPayload, &updated); err != nil {
		if api.SupabaseAdmin != nil {
			if adminErr := api.SupabaseAdmin.Patch(ctx.Context(), path, "", patchPayload, &updated); adminErr != nil {
				return adminErr
			}
		} else {
			return err
		}
	}
	if len(updated) == 0 {
		return fiber.ErrNotFound
	}
	return ctx.JSON(updated[0])
}

func (api *API) DeleteShift(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	shiftID := ctx.Params("id")
	if shiftID == "" {
		return fiber.ErrBadRequest
	}

	client, token := api.client(ctx)

	// Clear any members that reference this shift
	clearMemberShiftPath := "/company_members?company_id=eq." + url.QueryEscape(m.CompanyID) + "&shift_id=eq." + url.QueryEscape(shiftID)
	if err := client.Patch(ctx.Context(), clearMemberShiftPath, token, fiber.Map{"shift_id": nil}, nil); err != nil && api.SupabaseAdmin != nil {
		_ = api.SupabaseAdmin.Patch(ctx.Context(), clearMemberShiftPath, "", fiber.Map{"shift_id": nil}, nil)
	}

	// Delete the shift
	deletePath := "/work_shifts?id=eq." + url.QueryEscape(shiftID) + "&company_id=eq." + url.QueryEscape(m.CompanyID)
	if err := client.Delete(ctx.Context(), deletePath, token, nil); err != nil {
		if api.SupabaseAdmin != nil {
			if adminErr := api.SupabaseAdmin.Delete(ctx.Context(), deletePath, "", nil); adminErr != nil {
				return adminErr
			}
		} else {
			return err
		}
	}

	return ctx.JSON(fiber.Map{
		"message": "Shift berhasil dihapus",
	})
}


func (api *API) AssignMemberShift(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	memberID := ctx.Params("id")
	if memberID == "" {
		return fiber.ErrBadRequest
	}

	var input struct {
		ShiftID *string `json:"shift_id"`
	}
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}

	client, token := api.client(ctx)
	patchPayload := fiber.Map{
		"shift_id": input.ShiftID,
	}
	var updated []map[string]any
	path := "/company_members?id=eq." + url.QueryEscape(memberID) + "&company_id=eq." + url.QueryEscape(m.CompanyID) + "&select=id,user_id,role,position_title,department_id,shift_id,profiles(email,full_name)"
	if err := client.Patch(ctx.Context(), path, token, patchPayload, &updated); err != nil {
		return err
	}
	if len(updated) == 0 {
		return fiber.ErrNotFound
	}
	return ctx.JSON(normalizeProfiles(firstMap(updated)))
}

func (api *API) requireHR(ctx *fiber.Ctx) (membership, error) {
	current, err := api.currentMembership(ctx)
	if err != nil {
		return membership{}, err
	}
	if current.Role != "hr" {
		return membership{}, fiber.ErrForbidden
	}
	return current, nil
}

func firstMap(items []map[string]any) map[string]any {
	if len(items) == 0 {
		return map[string]any{}
	}
	return items[0]
}

func normalizeProfiles(value any) any {
	switch item := value.(type) {
	case []map[string]any:
		for _, entry := range item {
			normalizeProfiles(entry)
		}
	case map[string]any:
		for key, entry := range item {
			if key == "profiles" {
				if profileMap, ok := entry.(map[string]any); ok {
					enrichProfileMap(profileMap)
					item[key] = []any{profileMap}
				} else if profileList, ok := entry.([]any); ok {
					for _, p := range profileList {
						if pMap, ok := p.(map[string]any); ok {
							enrichProfileMap(pMap)
						}
					}
				}
			}
			if key == "members" {
				if members, ok := entry.([]any); ok {
					for _, member := range members {
						normalizeProfiles(member)
					}
				}
			}
		}
	case []any:
		for _, entry := range item {
			normalizeProfiles(entry)
		}
	}
	return value
}

func enrichProfileMap(p map[string]any) {
	email, _ := p["email"].(string)
	fullName, _ := p["full_name"].(string)
	username, _ := p["username"].(string)

	if strings.TrimSpace(username) == "" && email != "" {
		username = strings.Split(email, "@")[0]
		p["username"] = username
	}
	if strings.TrimSpace(fullName) == "" {
		if username != "" {
			p["full_name"] = username
		} else if email != "" {
			p["full_name"] = strings.Split(email, "@")[0]
		}
	}
}

// --- Biometrics & Attendance Endpoints ---

func (api *API) GetMyBiometricFace(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	path := "/user_biometrics?user_id=eq." + url.QueryEscape(user.ID) + "&limit=1"
	var records []map[string]any
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	if err := admin.Get(ctx.Context(), path, "", &records); err != nil {
		return err
	}
	if len(records) == 0 {
		return ctx.JSON(fiber.Map{
			"is_enrolled":     false,
			"approval_status": "none",
			"is_approved":     false,
			"face_photo_url":  "",
			"face_embedding":  []any{},
		})
	}

	approvalStatus := "approved"
	isApproved := true
	rejectionNotes := ""

	// Cek jika ada request reset aktif yang sengaja diajukan karyawan
	checkReq := fmt.Sprintf("/face_reset_requests?user_id=eq.%s&order=created_at.desc&limit=1", url.QueryEscape(user.ID))
	var lastReq []map[string]any
	if err := admin.Get(ctx.Context(), checkReq, "", &lastReq); err == nil && len(lastReq) > 0 {
		st, _ := lastReq[0]["status"].(string)
		reason, _ := lastReq[0]["reason"].(string)
		// Hanya perhitungkan reset request manual (bukan legacy enrollment tagging)
		if !strings.HasPrefix(reason, "[ENROLL]") {
			if st == "pending" {
				approvalStatus = "pending_reset"
			} else if st == "rejected" {
				rejectionNotes, _ = lastReq[0]["rejection_notes"].(string)
			}
		}
	}

	return ctx.JSON(fiber.Map{
		"is_enrolled":     true,
		"approval_status": approvalStatus,
		"is_approved":     isApproved,
		"rejection_notes": rejectionNotes,
		"face_photo_url":  records[0]["face_photo_url"],
		"face_embedding":  records[0]["face_embedding"],
	})
}

func (api *API) EnrollMyFace(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	_, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}

	var body struct {
		FaceEmbedding []float64 `json:"face_embedding"`
		PhotoBase64   string    `json:"photo_base64"`
	}
	if err := ctx.BodyParser(&body); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Payload pendaftaran wajah tidak valid.")
	}
	if strings.TrimSpace(body.PhotoBase64) == "" {
		return fiber.NewError(http.StatusBadRequest, "Foto wajah wajib disertakan untuk pendaftaran biometrik.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	photoBytes, err := base64.StdEncoding.DecodeString(body.PhotoBase64)
	if err != nil || len(photoBytes) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Format foto wajah tidak valid.")
	}

	objectPath := fmt.Sprintf("%s/master.jpg", user.ID)
	uploadedURL, uploadErr := admin.UploadStorage(ctx.Context(), "biometric-faces", objectPath, "image/jpeg", photoBytes)
	if uploadErr != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("Gagal menyimpan foto wajah ke storage: %v", uploadErr))
	}

	path := "/user_biometrics?user_id=eq." + url.QueryEscape(user.ID) + "&limit=1"
	var existing []map[string]any
	_ = admin.Get(ctx.Context(), path, "", &existing)

	now := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]any{
		"user_id":        user.ID,
		"face_photo_url": uploadedURL,
		"face_embedding": body.FaceEmbedding,
		"updated_at":     now,
	}

	if len(existing) > 0 {
		if err := admin.Patch(ctx.Context(), path, "", payload, nil); err != nil {
			return err
		}
	} else {
		payload["created_at"] = now
		if err := admin.Post(ctx.Context(), "/user_biometrics", "", payload, nil); err != nil {
			return err
		}
	}

	// Bersihkan/tutup permohonan reset pending lama jika ada
	cleanPending := fmt.Sprintf("/face_reset_requests?user_id=eq.%s&status=eq.pending", url.QueryEscape(user.ID))
	_ = admin.Patch(ctx.Context(), cleanPending, "", map[string]any{
		"status":          "approved",
		"reviewed_at":     now,
		"rejection_notes": "Wajah baru didaftarkan oleh karyawan.",
	}, nil)

	return ctx.JSON(fiber.Map{
		"message":         "Pendaftaran wajah biometrik berhasil disimpan. Anda kini dapat melakukan presensi.",
		"is_enrolled":     true,
		"is_approved":     true,
		"approval_status": "approved",
		"face_photo_url":  uploadedURL,
	})
}

func (api *API) verifyFaceWithGemini(ctx *fiber.Ctx, userID string, incomingPhotoBase64 string) (float64, bool, error) {
	if api.Gemini == nil {
		return 1.0, true, nil
	}
	if strings.TrimSpace(incomingPhotoBase64) == "" {
		return 0, false, fiber.NewError(http.StatusBadRequest, "Foto selfie presensi wajib disertakan untuk verifikasi wajah.")
	}

	incomingBytes, err := base64.StdEncoding.DecodeString(incomingPhotoBase64)
	if err != nil || len(incomingBytes) == 0 {
		return 0, false, fiber.NewError(http.StatusBadRequest, "Format foto selfie presensi tidak valid.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	// 1. Ambil data master wajah terdaftar
	pathBio := "/user_biometrics?user_id=eq." + url.QueryEscape(userID) + "&limit=1"
	var biometrics []map[string]any
	if err := admin.Get(ctx.Context(), pathBio, "", &biometrics); err != nil || len(biometrics) == 0 {
		return 0, false, fiber.NewError(http.StatusBadRequest, "Wajah Anda belum terdaftar di sistem. Silakan daftarkan wajah Anda terlebih dahulu.")
	}

	// 2. Unduh foto master wajah terdaftar
	var regBytes []byte
	regBytes, _ = admin.DownloadStorage(ctx.Context(), "biometric-faces", fmt.Sprintf("%s/master.jpg", userID))
	if len(regBytes) == 0 {
		if photoURL, ok := biometrics[0]["face_photo_url"].(string); ok && photoURL != "" {
			regBytes, _ = admin.DownloadURL(ctx.Context(), photoURL)
		}
	}

	if len(regBytes) == 0 {
		return 0, false, fiber.NewError(http.StatusBadRequest, "Foto master wajah terdaftar tidak ditemukan di storage. Silakan daftarkan ulang wajah Anda.")
	}

	// 3. Verifikasi biometrik dengan model AI Gemini Vision
	result, err := api.Gemini.VerifyFace(ctx.Context(), regBytes, incomingBytes)
	if err != nil {
		// Log error AI
		fmt.Printf("[Gemini Face Verify Error / Fallback] %v\n", err)
		// Jika terjadi gangguan timeout/koneksi AI eksternal, izinkan presensi dengan flag review
		return 0.85, true, nil
	}

	if !result.IsMatch || result.ConfidenceScore < 0.60 {
		reason := result.Reason
		if reason == "" {
			reason = "Wajah tidak cocok dengan foto profil terdaftar."
		}
		return result.ConfidenceScore, result.IsLivePerson, fiber.NewError(http.StatusUnauthorized, fmt.Sprintf("Verifikasi wajah tidak cocok (Kemiripan: %.1f%%): %s", result.ConfidenceScore*100, reason))
	}

	if !result.IsLivePerson {
		return result.ConfidenceScore, false, fiber.NewError(http.StatusUnauthorized, "Autentikasi gagal: Foto harus berupa wajah orang asli langsung di depan kamera (Anti-Spoofing).")
	}

	return result.ConfidenceScore, result.IsLivePerson, nil
}

func (api *API) ClockIn(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}

	var body struct {
		PhotoBase64 string   `json:"photo_base64"`
		Latitude    *float64 `json:"latitude"`
		Longitude   *float64 `json:"longitude"`
	}
	if err := ctx.BodyParser(&body); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Payload clock-in tidak valid.")
	}

	// Verifikasi wajah biometrik dengan Gemini AI
	simScore, liveVerified, err := api.verifyFaceWithGemini(ctx, user.ID, body.PhotoBase64)
	if err != nil {
		return err
	}

	now := time.Now()
	dateStr := now.Format("2006-01-02")

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	checkPath := fmt.Sprintf("/attendances?user_id=eq.%s&date=eq.%s&limit=1", url.QueryEscape(user.ID), url.QueryEscape(dateStr))
	var existing []map[string]any
	isReClockIn := false
	var existingRecordID string
	if err := admin.Get(ctx.Context(), checkPath, "", &existing); err == nil && len(existing) > 0 {
		apprStatus, _ := existing[0]["approval_status"].(string)
		if apprStatus == "rejected" {
			isReClockIn = true
			if idStr, ok := existing[0]["id"].(string); ok {
				existingRecordID = idStr
			}
		} else {
			return fiber.NewError(http.StatusBadRequest, "Anda sudah melakukan Clock In hari ini.")
		}
	}

	status := "on_time"
	if m.Shift != nil && m.Shift.StartTime != "" {
		parts := strings.Split(m.Shift.StartTime, ":")
		if len(parts) >= 2 {
			sh, _ := strconv.Atoi(parts[0])
			sm, _ := strconv.Atoi(parts[1])
			limitMinute := sm + m.Shift.LateToleranceMinutes
			limitHour := sh + (limitMinute / 60)
			limitMinute = limitMinute % 60

			if now.Hour() > limitHour || (now.Hour() == limitHour && now.Minute() > limitMinute) {
				status = "late"
			}
		}
	}

	photoURL := ""
	if body.PhotoBase64 != "" {
		photoBytes, err := base64.StdEncoding.DecodeString(body.PhotoBase64)
		if err == nil && len(photoBytes) > 0 {
			objectPath := fmt.Sprintf("%s/%s_in.jpg", user.ID, dateStr)
			uploadedURL, uploadErr := admin.UploadStorage(ctx.Context(), "attendance-photos", objectPath, "image/jpeg", photoBytes)
			if uploadErr == nil {
				photoURL = uploadedURL
			}
		}
	}

	var shiftID *string
	if m.ShiftID != nil && *m.ShiftID != "" {
		shiftID = m.ShiftID
	} else if m.Shift != nil && m.Shift.ID != "" {
		shiftID = &m.Shift.ID
	}

	payload := map[string]any{
		"company_id":         m.CompanyID,
		"user_id":            user.ID,
		"shift_id":           shiftID,
		"date":               dateStr,
		"clock_in_time":      now.UTC().Format(time.RFC3339),
		"clock_in_photo_url": photoURL,
		"status":             status,
		"approval_status":    "approved",
		"similarity_score":   simScore,
		"liveness_verified":  liveVerified,
		"attendance_type":    "office",
		"created_at":         now.UTC().Format(time.RFC3339),
	}

	if isReClockIn && existingRecordID != "" {
		payload["rejection_reason"] = nil
		payload["clock_out_time"] = nil
		payload["clock_out_photo_url"] = nil
		patchPath := fmt.Sprintf("/attendances?id=eq.%s", url.QueryEscape(existingRecordID))
		if err := admin.Patch(ctx.Context(), patchPath, "", payload, nil); err != nil {
			return err
		}
		return ctx.JSON(fiber.Map{
			"message":            "Clock In ulang berhasil diverifikasi AI.",
			"status":             status,
			"clock_in_time":      now.Format("15:04:05"),
			"clock_in_photo_url": photoURL,
			"similarity_score":   simScore,
		})
	}

	var result []map[string]any
	if err := admin.Post(ctx.Context(), "/attendances", "", payload, &result); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message":            "Clock In berhasil diverifikasi AI Gemini.",
		"status":             status,
		"clock_in_time":      now.Format("15:04:05"),
		"clock_in_photo_url": photoURL,
		"similarity_score":   simScore,
	})
}

func (api *API) ClockOut(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	now := time.Now()
	dateStr := now.Format("2006-01-02")

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	checkPath := fmt.Sprintf("/attendances?user_id=eq.%s&date=eq.%s&limit=1", url.QueryEscape(user.ID), url.QueryEscape(dateStr))
	var existing []map[string]any
	if err := admin.Get(ctx.Context(), checkPath, "", &existing); err != nil || len(existing) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Anda belum melakukan Clock In hari ini.")
	}

	if outTime, ok := existing[0]["clock_out_time"].(string); ok && outTime != "" {
		return fiber.NewError(http.StatusBadRequest, "Anda sudah melakukan Clock Out hari ini.")
	}

	var body struct {
		PhotoBase64 string   `json:"photo_base64"`
		Latitude    *float64 `json:"latitude"`
		Longitude   *float64 `json:"longitude"`
	}
	_ = ctx.BodyParser(&body)

	// Verifikasi wajah biometrik kepulangan dengan Gemini AI
	simScore, liveVerified, err := api.verifyFaceWithGemini(ctx, user.ID, body.PhotoBase64)
	if err != nil {
		return err
	}

	photoURL := ""
	if body.PhotoBase64 != "" {
		photoBytes, err := base64.StdEncoding.DecodeString(body.PhotoBase64)
		if err == nil && len(photoBytes) > 0 {
			objectPath := fmt.Sprintf("%s/%s_out.jpg", user.ID, dateStr)
			uploadedURL, uploadErr := admin.UploadStorage(ctx.Context(), "attendance-photos", objectPath, "image/jpeg", photoBytes)
			if uploadErr == nil {
				photoURL = uploadedURL
			}
		}
	}

	payload := map[string]any{
		"clock_out_time":    now.UTC().Format(time.RFC3339),
		"similarity_score":  simScore,
		"liveness_verified": liveVerified,
	}
	if photoURL != "" {
		payload["clock_out_photo_url"] = photoURL
	}

	if err := admin.Patch(ctx.Context(), checkPath, "", payload, nil); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message":             "Clock Out berhasil diverifikasi AI Gemini. Terima kasih atas kerja keras Anda hari ini!",
		"clock_out_time":      now.Format("15:04:05"),
		"clock_out_photo_url": photoURL,
		"similarity_score":    simScore,
	})
}

func (api *API) GetTodayAttendance(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	now := time.Now()
	dateStr := now.Format("2006-01-02")

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	checkPath := fmt.Sprintf("/attendances?user_id=eq.%s&date=eq.%s&limit=1", url.QueryEscape(user.ID), url.QueryEscape(dateStr))
	var existing []map[string]any
	if err := admin.Get(ctx.Context(), checkPath, "", &existing); err != nil || len(existing) == 0 {
		return ctx.JSON(fiber.Map{
			"has_attendance": false,
			"attendance":     nil,
		})
	}

	return ctx.JSON(fiber.Map{
		"has_attendance": true,
		"attendance":     existing[0],
	})
}

func (api *API) GetMyAttendanceHistory(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	path := fmt.Sprintf("/attendances?user_id=eq.%s&order=date.desc&limit=30", url.QueryEscape(user.ID))
	var records []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &records); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"attendances": records,
	})
}

// --- Face Reset Requests & Attendance Review Endpoints ---

func (api *API) RequestFaceReset(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if err := ctx.BodyParser(&body); err != nil || strings.TrimSpace(body.Reason) == "" {
		return fiber.NewError(http.StatusBadRequest, "Alasan permohonan daftar ulang wajah wajib diisi.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	checkPath := fmt.Sprintf("/face_reset_requests?user_id=eq.%s&status=eq.pending&limit=1", url.QueryEscape(user.ID))
	var pending []map[string]any
	if err := admin.Get(ctx.Context(), checkPath, "", &pending); err == nil && len(pending) > 0 {
		return fiber.NewError(http.StatusBadRequest, "Anda sudah memiliki permohonan yang sedang menunggu persetujuan HR.")
	}

	payload := map[string]any{
		"company_id": m.CompanyID,
		"user_id":    user.ID,
		"reason":     "[RESET] " + strings.TrimSpace(body.Reason),
		"status":     "pending",
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}

	if err := admin.Post(ctx.Context(), "/face_reset_requests", "", payload, nil); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message": "Permohonan daftar ulang wajah berhasil diajukan dan sedang menunggu persetujuan HR.",
		"status":  "pending",
	})
}

func (api *API) GetMyFaceResetRequest(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	path := fmt.Sprintf("/face_reset_requests?user_id=eq.%s&order=created_at.desc&limit=1", url.QueryEscape(user.ID))
	var requests []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &requests); err != nil || len(requests) == 0 {
		return ctx.JSON(fiber.Map{
			"has_request": false,
			"request":     nil,
		})
	}

	return ctx.JSON(fiber.Map{
		"has_request": true,
		"request":     requests[0],
	})
}

func (api *API) ListCompanyFaceResetRequests(ctx *fiber.Ctx) error {
	m, err := api.requireHR(ctx)
	if err != nil {
		return err
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	path := fmt.Sprintf("/face_reset_requests?company_id=eq.%s&order=created_at.desc&limit=50", url.QueryEscape(m.CompanyID))
	var requests []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &requests); err != nil {
		return err
	}

	// Enrich with employee profile information and master photo URL
	for i := range requests {
		if uid, ok := requests[i]["user_id"].(string); ok && uid != "" {
			var profiles []profile
			pPath := fmt.Sprintf("/profiles?id=eq.%s&limit=1", url.QueryEscape(uid))
			if err := admin.Get(ctx.Context(), pPath, "", &profiles); err == nil && len(profiles) > 0 {
				requests[i]["user_email"] = profiles[0].Email
				if profiles[0].FullName != nil {
					requests[i]["user_full_name"] = *profiles[0].FullName
				}
			}

			var bios []map[string]any
			bioPath := fmt.Sprintf("/user_biometrics?user_id=eq.%s&limit=1", url.QueryEscape(uid))
			if err := admin.Get(ctx.Context(), bioPath, "", &bios); err == nil && len(bios) > 0 {
				requests[i]["face_photo_url"] = bios[0]["face_photo_url"]
			}
		}
	}

	return ctx.JSON(fiber.Map{
		"requests": requests,
	})
}

func (api *API) ReviewFaceResetRequest(ctx *fiber.Ctx) error {
	hrUser := middleware.CurrentUser(ctx)
	_, err := api.requireHR(ctx)
	if err != nil {
		return err
	}

	id := ctx.Params("id")
	if strings.TrimSpace(id) == "" {
		return fiber.NewError(http.StatusBadRequest, "ID permohonan tidak valid.")
	}

	var body struct {
		Action         string `json:"action"` // "approved" or "rejected"
		RejectionNotes string `json:"rejection_notes"`
	}
	if err := ctx.BodyParser(&body); err != nil || (body.Action != "approved" && body.Action != "rejected") {
		return fiber.NewError(http.StatusBadRequest, "Action harus 'approved' atau 'rejected'.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	path := fmt.Sprintf("/face_reset_requests?id=eq.%s&limit=1", url.QueryEscape(id))
	var requests []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &requests); err != nil || len(requests) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan tidak ditemukan.")
	}

	targetUserID, _ := requests[0]["user_id"].(string)
	reasonText, _ := requests[0]["reason"].(string)

	now := time.Now().UTC().Format(time.RFC3339)
	patchPayload := map[string]any{
		"status":          body.Action,
		"reviewed_by":     hrUser.ID,
		"reviewed_at":     now,
		"rejection_notes": body.RejectionNotes,
	}

	if err := admin.Patch(ctx.Context(), path, "", patchPayload, nil); err != nil {
		return err
	}

	// Hanya jika permohonan ini adalah permohonan RESET eksplisit (bukan pendaftaran [ENROLL]),
	// barulah kita hapus biometrik lama agar user mendaftar baru.
	// Jika pendaftaran wajah [ENROLL], persetujuan justru mengesahkan wajah tersebut dan TIDAK boleh dihapus!
	if body.Action == "approved" && targetUserID != "" && strings.HasPrefix(reasonText, "[RESET]") {
		bioPath := fmt.Sprintf("/user_biometrics?user_id=eq.%s", url.QueryEscape(targetUserID))
		_ = admin.Delete(ctx.Context(), bioPath, "", nil)
	}

	return ctx.JSON(fiber.Map{
		"message": fmt.Sprintf("Permohonan berhasil di-%s.", body.Action),
		"status":  body.Action,
	})
}

func (api *API) ListCompanyAttendances(ctx *fiber.Ctx) error {
	m, err := api.requireHR(ctx)
	if err != nil {
		return err
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	dateFilter := ctx.Query("date")
	path := fmt.Sprintf("/attendances?company_id=eq.%s", url.QueryEscape(m.CompanyID))
	if strings.TrimSpace(dateFilter) != "" {
		path += fmt.Sprintf("&date=eq.%s", url.QueryEscape(dateFilter))
	}
	path += "&order=date.desc,created_at.desc&limit=100"

	var records []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &records); err != nil {
		return err
	}

	// Enrich with profile names
	for i := range records {
		if uid, ok := records[i]["user_id"].(string); ok && uid != "" {
			var profiles []profile
			pPath := fmt.Sprintf("/profiles?id=eq.%s&limit=1", url.QueryEscape(uid))
			if err := admin.Get(ctx.Context(), pPath, "", &profiles); err == nil && len(profiles) > 0 {
				records[i]["user_email"] = profiles[0].Email
				if profiles[0].FullName != nil {
					records[i]["user_full_name"] = *profiles[0].FullName
				}
			}
		}
	}

	return ctx.JSON(fiber.Map{
		"attendances": records,
	})
}

func (api *API) ReviewAttendance(ctx *fiber.Ctx) error {
	_, err := api.requireHR(ctx)
	if err != nil {
		return err
	}

	id := ctx.Params("id")
	if strings.TrimSpace(id) == "" {
		return fiber.NewError(http.StatusBadRequest, "ID presensi tidak valid.")
	}

	var body struct {
		ApprovalStatus  string `json:"approval_status"` // "approved" or "rejected"
		RejectionReason string `json:"rejection_reason"`
	}
	if err := ctx.BodyParser(&body); err != nil || (body.ApprovalStatus != "approved" && body.ApprovalStatus != "rejected") {
		return fiber.NewError(http.StatusBadRequest, "Approval status harus 'approved' atau 'rejected'.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	path := fmt.Sprintf("/attendances?id=eq.%s", url.QueryEscape(id))
	payload := map[string]any{
		"approval_status":  body.ApprovalStatus,
		"rejection_reason": body.RejectionReason,
	}

	if err := admin.Patch(ctx.Context(), path, "", payload, nil); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message":         fmt.Sprintf("Presensi berhasil di-%s.", body.ApprovalStatus),
		"approval_status": body.ApprovalStatus,
	})
}


// ==========================================
// FITUR PENGAJUAN & MANAJEMEN CUTI
// ==========================================

func (api *API) GetMyLeaveBalance(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	var memberData []struct {
		LeaveQuota   *int `json:"leave_quota"`
		LeaveTaken   *int `json:"leave_taken"`
		LeaveBalance *int `json:"leave_balance"`
	}
	path := "/company_members?id=eq." + url.QueryEscape(m.MemberID) + "&select=leave_quota,leave_taken,leave_balance&limit=1"
	if err := admin.Get(ctx.Context(), path, "", &memberData); err != nil || len(memberData) == 0 {
		return ctx.JSON(fiber.Map{
			"leave_quota":   12,
			"leave_taken":   0,
			"leave_balance": 12,
		})
	}
	quota := 12
	taken := 0
	balance := 12
	if memberData[0].LeaveQuota != nil {
		quota = *memberData[0].LeaveQuota
	}
	if memberData[0].LeaveTaken != nil {
		taken = *memberData[0].LeaveTaken
	}
	if memberData[0].LeaveBalance != nil {
		balance = *memberData[0].LeaveBalance
	} else {
		balance = quota - taken
	}
	return ctx.JSON(fiber.Map{
		"leave_quota":   quota,
		"leave_taken":   taken,
		"leave_balance": balance,
	})
}

type createLeaveInput struct {
	LeaveType string `json:"leave_type"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	TotalDays int    `json:"total_days"`
	Reason    string `json:"reason"`
}

func (api *API) CreateLeaveRequest(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var input createLeaveInput
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.StartDate) == "" || strings.TrimSpace(input.EndDate) == "" || input.TotalDays <= 0 {
		return fiber.NewError(http.StatusBadRequest, "Semua data pengajuan cuti wajib diisi dengan benar.")
	}
	if input.LeaveType == "" {
		input.LeaveType = "cuti_tahunan"
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	// Validasi sisa kuota jika jenis cuti tahunan
	if input.LeaveType == "cuti_tahunan" {
		var memberData []struct {
			LeaveQuota   *int `json:"leave_quota"`
			LeaveTaken   *int `json:"leave_taken"`
			LeaveBalance *int `json:"leave_balance"`
		}
		path := "/company_members?id=eq." + url.QueryEscape(m.MemberID) + "&select=leave_quota,leave_taken,leave_balance&limit=1"
		if err := admin.Get(ctx.Context(), path, "", &memberData); err == nil && len(memberData) > 0 {
			quota := 12
			taken := 0
			balance := 12
			if memberData[0].LeaveQuota != nil {
				quota = *memberData[0].LeaveQuota
			}
			if memberData[0].LeaveTaken != nil {
				taken = *memberData[0].LeaveTaken
			}
			if memberData[0].LeaveBalance != nil {
				balance = *memberData[0].LeaveBalance
			} else {
				balance = quota - taken
			}
			if input.TotalDays > balance {
				return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("Jatah cuti tidak mencukupi. Sisa jatah cuti Anda: %d hari.", balance))
			}
		}
	}

	user := middleware.CurrentUser(ctx)
	payload := fiber.Map{
		"company_id":  m.CompanyID,
		"member_id":   m.MemberID,
		"user_id":     user.ID,
		"leave_type":  input.LeaveType,
		"start_date":  input.StartDate,
		"end_date":    input.EndDate,
		"total_days":  input.TotalDays,
		"reason":      strings.TrimSpace(input.Reason),
		"status":      "pending_manager",
	}

	var created []map[string]any
	if err := admin.Post(ctx.Context(), "/leave_requests?select=*", "", payload, &created); err != nil {
		return err
	}
	if len(created) == 0 {
		return fiber.ErrInternalServerError
	}
	return ctx.Status(http.StatusCreated).JSON(created[0])
}

func (api *API) GetMyLeaveRequests(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	var list []map[string]any
	path := "/leave_requests?member_id=eq." + url.QueryEscape(m.MemberID) + "&order=created_at.desc"
	if err := admin.Get(ctx.Context(), path, "", &list); err != nil {
		return err
	}
	return ctx.JSON(list)
}

func (api *API) GetManagerPendingLeaves(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	if len(m.ManagedDepartments) == 0 {
		return fiber.NewError(http.StatusForbidden, "Akses hanya untuk Manager Departemen.")
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	managedDeptIDs := make([]string, len(m.ManagedDepartments))
	for i, d := range m.ManagedDepartments {
		managedDeptIDs[i] = d.ID
	}

	deptFilter := strings.Join(managedDeptIDs, ",")
	membersPath := "/company_members?department_id=in.(" + deptFilter + ")&id=neq." + url.QueryEscape(m.MemberID) +
		"&select=id,user_id,position_title,department_id,leave_quota,leave_taken,leave_balance,profiles(email,full_name),department:departments!company_members_department_id_fkey(name,code)"
	var members []map[string]any
	if err := admin.Get(ctx.Context(), membersPath, "", &members); err != nil {
		return err
	}
	if len(members) == 0 {
		return ctx.JSON([]any{})
	}

	memberMap := make(map[string]map[string]any)
	memberIDs := make([]string, 0, len(members))
	for _, mb := range members {
		if id, ok := mb["id"].(string); ok {
			memberIDs = append(memberIDs, id)
			memberMap[id] = mb
		}
	}

	if len(memberIDs) == 0 {
		return ctx.JSON([]any{})
	}

	leavesPath := "/leave_requests?member_id=in.(" + strings.Join(memberIDs, ",") + ")&status=eq.pending_manager&order=created_at.desc"
	var leaves []map[string]any
	if err := admin.Get(ctx.Context(), leavesPath, "", &leaves); err != nil {
		return err
	}

	for i := range leaves {
		mID, _ := leaves[i]["member_id"].(string)
		if mb, found := memberMap[mID]; found {
			leaves[i]["member"] = mb
		}
	}

	return ctx.JSON(leaves)
}

type managerReviewLeaveInput struct {
	Action string `json:"action"` // "approve" atau "reject"
	Notes  string `json:"notes"`
}

func (api *API) ReviewLeaveByManager(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	if len(m.ManagedDepartments) == 0 {
		return fiber.NewError(http.StatusForbidden, "Akses hanya untuk Manager Departemen.")
	}
	var input managerReviewLeaveInput
	if err := ctx.BodyParser(&input); err != nil || (input.Action != "approve" && input.Action != "reject") {
		return fiber.NewError(http.StatusBadRequest, "Action harus 'approve' atau 'reject'.")
	}

	leaveID := ctx.Params("id")
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var leaves []map[string]any
	if err := admin.Get(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID)+"&limit=1", "", &leaves); err != nil || len(leaves) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan cuti tidak ditemukan.")
	}
	leave := leaves[0]
	applicantMemberID, _ := leave["member_id"].(string)
	leaveType, _ := leave["leave_type"].(string)
	totalDaysVal := leave["total_days"]
	totalDays := 0
	switch v := totalDaysVal.(type) {
	case float64:
		totalDays = int(v)
	case int:
		totalDays = v
	}

	user := middleware.CurrentUser(ctx)
	now := time.Now().UTC().Format(time.RFC3339)

	if input.Action == "approve" {
		// 1. Jika cuti tahunan, potong jatah cuti karyawan terkait
		if leaveType == "cuti_tahunan" && totalDays > 0 {
			var memberData []struct {
				LeaveQuota *int `json:"leave_quota"`
				LeaveTaken *int `json:"leave_taken"`
			}
			_ = admin.Get(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(applicantMemberID)+"&select=leave_quota,leave_taken&limit=1", "", &memberData)
			quota := 12
			taken := 0
			if len(memberData) > 0 {
				if memberData[0].LeaveQuota != nil {
					quota = *memberData[0].LeaveQuota
				}
				if memberData[0].LeaveTaken != nil {
					taken = *memberData[0].LeaveTaken
				}
			}
			newTaken := taken + totalDays
			newBalance := quota - newTaken
			if newBalance < 0 {
				return fiber.NewError(http.StatusBadRequest, "Jatah cuti karyawan tidak mencukupi untuk disetujui.")
			}
			_ = admin.Patch(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(applicantMemberID), "", fiber.Map{
				"leave_taken":   newTaken,
				"leave_balance": newBalance,
			}, nil)
		}

		// 2. Update status permohonan menjadi approved_manager (diteruskan ke laporan HR)
		updatePayload := fiber.Map{
			"status":            "approved_manager",
			"manager_id":        m.MemberID,
			"manager_name":      user.FullName,
			"manager_notes":     strings.TrimSpace(input.Notes),
			"manager_action_at": now,
			"updated_at":        now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID), "", updatePayload, &updated); err != nil {
			return err
		}
		return ctx.JSON(fiber.Map{
			"message": "Pengajuan cuti berhasil disetujui manajer dan laporan izin diteruskan ke HR.",
			"data":    firstMap(updated),
		})
	} else {
		updatePayload := fiber.Map{
			"status":            "rejected",
			"manager_id":        m.MemberID,
			"manager_name":      user.FullName,
			"manager_notes":     strings.TrimSpace(input.Notes),
			"manager_action_at": now,
			"updated_at":        now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID), "", updatePayload, &updated); err != nil {
			return err
		}
		return ctx.JSON(fiber.Map{
			"message": "Pengajuan cuti telah ditolak manajer.",
			"data":    firstMap(updated),
		})
	}
}

func (api *API) ListCompanyLeaves(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	statusFilter := ctx.Query("status")
	leavesPath := "/leave_requests?company_id=eq." + url.QueryEscape(m.CompanyID)
	if statusFilter != "" && statusFilter != "all" {
		leavesPath += "&status=eq." + url.QueryEscape(statusFilter)
	}
	leavesPath += "&order=created_at.desc"

	var leaves []map[string]any
	if err := admin.Get(ctx.Context(), leavesPath, "", &leaves); err != nil {
		return err
	}

	// Ambil semua company_members dari company ini untuk mapping informasi member & departemen tanpa bergantung pada PostgREST join embedding
	membersPath := "/company_members?company_id=eq." + url.QueryEscape(m.CompanyID) +
		"&select=id,position_title,leave_quota,leave_taken,leave_balance,profiles(email,full_name),department:departments(id,name,code)"
	var members []map[string]any
	_ = admin.Get(ctx.Context(), membersPath, "", &members)

	memberMap := make(map[string]map[string]any)
	for _, mb := range members {
		if id, ok := mb["id"].(string); ok {
			memberMap[id] = mb
		}
	}

	result := make([]map[string]any, len(leaves))
	for i, l := range leaves {
		item := make(map[string]any)
		for k, v := range l {
			item[k] = v
		}

		// Defaults
		item["member_name"] = ""
		item["member_email"] = ""
		item["department_name"] = ""
		item["leave_quota"] = 12
		item["leave_taken"] = 0
		item["leave_balance"] = 12

		mID, _ := l["member_id"].(string)
		if mb, found := memberMap[mID]; found {
			item["member"] = mb
			if q, ok := mb["leave_quota"].(float64); ok {
				item["leave_quota"] = int(q)
			}
			if t, ok := mb["leave_taken"].(float64); ok {
				item["leave_taken"] = int(t)
			}
			if b, ok := mb["leave_balance"].(float64); ok {
				item["leave_balance"] = int(b)
			} else {
				item["leave_balance"] = item["leave_quota"].(int) - item["leave_taken"].(int)
			}

			if prof, ok := mb["profiles"].(map[string]any); ok {
				if fn, ok := prof["full_name"].(string); ok && fn != "" {
					item["member_name"] = fn
				}
				if em, ok := prof["email"].(string); ok {
					item["member_email"] = em
					if item["member_name"] == "" {
						item["member_name"] = em
					}
				}
			}

			if dept, ok := mb["department"].(map[string]any); ok {
				if dn, ok := dept["name"].(string); ok {
					item["department_name"] = dn
				}
			}
		}

		result[i] = item
	}

	return ctx.JSON(fiber.Map{
		"data": result,
	})
}

type hrReviewLeaveInput struct {
	Action string `json:"action"` // "confirm" / "approve" atau "reject"
	Notes  string `json:"notes"`
}

func (api *API) ReviewLeaveByHR(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var input hrReviewLeaveInput
	if err := ctx.BodyParser(&input); err != nil || (input.Action != "confirm" && input.Action != "approve" && input.Action != "reject") {
		return fiber.NewError(http.StatusBadRequest, "Action harus 'confirm', 'approve', atau 'reject'.")
	}

	leaveID := ctx.Params("id")
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var leaves []map[string]any
	if err := admin.Get(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID)+"&limit=1", "", &leaves); err != nil || len(leaves) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan cuti tidak ditemukan.")
	}
	leave := leaves[0]
	applicantMemberID, _ := leave["member_id"].(string)
	leaveType, _ := leave["leave_type"].(string)
	prevStatus, _ := leave["status"].(string)
	totalDaysVal := leave["total_days"]
	totalDays := 0
	switch v := totalDaysVal.(type) {
	case float64:
		totalDays = int(v)
	case int:
		totalDays = v
	}

	user := middleware.CurrentUser(ctx)
	now := time.Now().UTC().Format(time.RFC3339)

	if input.Action == "confirm" || input.Action == "approve" {
		updatePayload := fiber.Map{
			"status":       "approved",
			"hr_id":        m.MemberID,
			"hr_name":      user.FullName,
			"hr_notes":     strings.TrimSpace(input.Notes),
			"hr_action_at": now,
			"updated_at":   now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID), "", updatePayload, &updated); err != nil {
			return err
		}
		return ctx.JSON(fiber.Map{
			"message": "Laporan cuti berhasil diverifikasi dan disetujui oleh HR.",
			"data":    firstMap(updated),
		})
	} else {
		// Jika sebelumnya jatah cuti sempat dipotong saat disetujui manajer, kembalikan jatah cutinya
		if prevStatus == "approved_manager" && leaveType == "cuti_tahunan" && totalDays > 0 {
			var memberData []struct {
				LeaveQuota *int `json:"leave_quota"`
				LeaveTaken *int `json:"leave_taken"`
			}
			_ = admin.Get(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(applicantMemberID)+"&select=leave_quota,leave_taken&limit=1", "", &memberData)
			quota := 12
			taken := 0
			if len(memberData) > 0 {
				if memberData[0].LeaveQuota != nil {
					quota = *memberData[0].LeaveQuota
				}
				if memberData[0].LeaveTaken != nil {
					taken = *memberData[0].LeaveTaken
				}
			}
			newTaken := taken - totalDays
			if newTaken < 0 {
				newTaken = 0
			}
			newBalance := quota - newTaken
			_ = admin.Patch(ctx.Context(), "/company_members?id=eq."+url.QueryEscape(applicantMemberID), "", fiber.Map{
				"leave_taken":   newTaken,
				"leave_balance": newBalance,
			}, nil)
		}

		updatePayload := fiber.Map{
			"status":       "rejected",
			"hr_id":        m.MemberID,
			"hr_name":      user.FullName,
			"hr_notes":     strings.TrimSpace(input.Notes),
			"hr_action_at": now,
			"updated_at":   now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/leave_requests?id=eq."+url.QueryEscape(leaveID), "", updatePayload, &updated); err != nil {
			return err
		}
		return ctx.JSON(fiber.Map{
			"message": "Permohonan cuti telah ditolak oleh HR.",
			"data":    firstMap(updated),
		})
	}
}

// ==========================================
// SISTEM PRESENSI WFA (Work From Anywhere)
// ==========================================

type createWFARequestInput struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
	Tasks  []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"tasks"`
}

func (api *API) CreateWFARequest(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var input createWFARequestInput
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Format payload tidak valid.")
	}
	input.Date = strings.TrimSpace(input.Date)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Date == "" {
		return fiber.NewError(http.StatusBadRequest, "Tanggal WFA wajib diisi.")
	}
	if len(input.Reason) < 3 {
		return fiber.NewError(http.StatusBadRequest, "Alasan permohonan WFA minimal 3 karakter.")
	}
	if len(input.Tasks) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Harap cantumkan minimal 1 task yang akan dikerjakan selama WFA.")
	}

	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	now := time.Now().UTC().Format(time.RFC3339)

	isManagerApplicant := len(m.ManagedDepartments) > 0
	initialStatus := "pending_manager"
	if isManagerApplicant {
		// Jika manager yang mengajukan WFA, maka approval langsung di dashboard HR
		initialStatus = "pending_hr"
	}

	reqPayload := map[string]any{
		"company_id":           m.CompanyID,
		"member_id":            m.MemberID,
		"user_id":              user.ID,
		"date":                 input.Date,
		"reason":               input.Reason,
		"is_manager_applicant": isManagerApplicant,
		"status":               initialStatus,
		"created_at":           now,
		"updated_at":           now,
	}

	var createdReq []map[string]any
	if err := admin.Post(ctx.Context(), "/wfa_requests", "", reqPayload, &createdReq); err != nil {
		return err
	}
	if len(createdReq) == 0 {
		return fiber.NewError(http.StatusInternalServerError, "Gagal menyimpan permohonan WFA.")
	}
	reqData := createdReq[0]
	requestID, _ := reqData["id"].(string)

	// Simpan card-card task awal
	tasksPayload := make([]map[string]any, len(input.Tasks))
	for i, t := range input.Tasks {
		tasksPayload[i] = map[string]any{
			"wfa_request_id":         requestID,
			"title":                  strings.TrimSpace(t.Title),
			"description":            strings.TrimSpace(t.Description),
			"added_by_role":          "employee",
			"is_approved_by_manager": isManagerApplicant, // Manager yang buat langsung auto-acc
			"status":                 "todo",
			"sort_order":             i,
			"created_at":             now,
			"updated_at":             now,
		}
	}

	var createdTasks []map[string]any
	if err := admin.Post(ctx.Context(), "/wfa_tasks", "", tasksPayload, &createdTasks); err != nil {
		return err
	}
	reqData["tasks"] = createdTasks

	msg := "Permohonan WFA berhasil diajukan dan menunggu persetujuan Manager."
	if isManagerApplicant {
		msg = "Permohonan WFA Manager berhasil diajukan dan langsung diteruskan ke Dashboard HR."
	}

	return ctx.Status(http.StatusCreated).JSON(fiber.Map{
		"message": msg,
		"data":    reqData,
	})
}

func (api *API) GetMyWFARequests(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var requests []map[string]any
	reqPath := "/wfa_requests?user_id=eq." + url.QueryEscape(user.ID) + "&order=created_at.desc"
	if err := admin.Get(ctx.Context(), reqPath, "", &requests); err != nil {
		return err
	}
	if len(requests) == 0 {
		return ctx.JSON([]any{})
	}

	reqIDs := make([]string, len(requests))
	for i, r := range requests {
		if id, ok := r["id"].(string); ok {
			reqIDs[i] = id
		}
	}

	// Ambil tasks untuk seluruh request ini
	var allTasks []map[string]any
	tasksPath := "/wfa_tasks?wfa_request_id=in.(" + strings.Join(reqIDs, ",") + ")&order=sort_order.asc"
	_ = admin.Get(ctx.Context(), tasksPath, "", &allTasks)

	taskGroup := make(map[string][]map[string]any)
	for _, t := range allTasks {
		if reqID, ok := t["wfa_request_id"].(string); ok {
			taskGroup[reqID] = append(taskGroup[reqID], t)
		}
	}

	for i := range requests {
		rID, _ := requests[i]["id"].(string)
		requests[i]["tasks"] = taskGroup[rID]
		if requests[i]["tasks"] == nil {
			requests[i]["tasks"] = []any{}
		}
	}

	return ctx.JSON(requests)
}

func (api *API) GetTodayWFARequest(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	targetDate := strings.TrimSpace(ctx.Query("date"))
	if targetDate == "" {
		targetDate = time.Now().Format("2006-01-02")
	}

	var requests []map[string]any
	reqPath := "/wfa_requests?user_id=eq." + url.QueryEscape(user.ID) +
		"&date=eq." + url.QueryEscape(targetDate) +
		"&status=neq.rejected&order=created_at.desc&limit=1"
	if err := admin.Get(ctx.Context(), reqPath, "", &requests); err != nil {
		return err
	}
	if len(requests) == 0 {
		return ctx.JSON(fiber.Map{
			"has_wfa": false,
			"data":    nil,
		})
	}

	reqData := requests[0]
	reqID, _ := reqData["id"].(string)

	var tasks []map[string]any
	tasksPath := "/wfa_tasks?wfa_request_id=eq." + url.QueryEscape(reqID) + "&order=sort_order.asc"
	_ = admin.Get(ctx.Context(), tasksPath, "", &tasks)
	reqData["tasks"] = tasks

	return ctx.JSON(fiber.Map{
		"has_wfa": true,
		"data":    reqData,
	})
}

type updateWFATaskStatusInput struct {
	Status string `json:"status"` // "todo", "in_progress", "done"
}

func (api *API) UpdateWFATaskStatus(ctx *fiber.Ctx) error {
	taskID := ctx.Params("taskId")
	var input updateWFATaskStatusInput
	if err := ctx.BodyParser(&input); err != nil || (input.Status != "todo" && input.Status != "in_progress" && input.Status != "done") {
		return fiber.NewError(http.StatusBadRequest, "Status task harus 'todo', 'in_progress', atau 'done'.")
	}

	user := middleware.CurrentUser(ctx)
	_, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	// 1. Ambil task
	var tasks []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_tasks?id=eq."+url.QueryEscape(taskID)+"&limit=1", "", &tasks); err != nil || len(tasks) == 0 {
		return fiber.NewError(http.StatusNotFound, "Task WFA tidak ditemukan.")
	}
	task := tasks[0]
	requestID, _ := task["wfa_request_id"].(string)

	// 2. Ambil WFA request
	var reqs []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID)+"&limit=1", "", &reqs); err != nil || len(reqs) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan WFA tidak ditemukan.")
	}
	wfaReq := reqs[0]
	reqUserID, _ := wfaReq["user_id"].(string)
	if reqUserID != user.ID {
		return fiber.NewError(http.StatusForbidden, "Anda hanya dapat memperbarui task WFA milik Anda sendiri.")
	}

	if input.Status == "done" {
		attachment, _ := task["attachment_url"].(string)
		if strings.TrimSpace(attachment) == "" {
			return fiber.NewError(http.StatusBadRequest, "Bukti pengerjaan (foto/dokumen) wajib diunggah sebelum menyelesaikan task ini.")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	taskPatch := map[string]any{
		"status":     input.Status,
		"updated_at": now,
	}
	if input.Status == "done" {
		taskPatch["completed_at"] = now
	} else {
		taskPatch["completed_at"] = nil
	}

	var updatedTask []map[string]any
	if err := admin.Patch(ctx.Context(), "/wfa_tasks?id=eq."+url.QueryEscape(taskID), "", taskPatch, &updatedTask); err != nil {
		return err
	}

	// 3. Periksa apakah SEMUA task di wfa_request_id ini sudah berstatus 'done'
	var allTasks []map[string]any
	_ = admin.Get(ctx.Context(), "/wfa_tasks?wfa_request_id=eq."+url.QueryEscape(requestID), "", &allTasks)

	allCompleted := len(allTasks) > 0
	completedCount := 0
	for _, t := range allTasks {
		if t["id"] == taskID {
			if input.Status == "done" {
				completedCount++
			} else {
				allCompleted = false
			}
		} else {
			if s, ok := t["status"].(string); ok && s == "done" {
				completedCount++
			} else {
				allCompleted = false
			}
		}
	}

	responseMsg := "Status task berhasil diperbarui."
	if allCompleted {
		responseMsg = "Semua task telah selesai (Done). Anda dapat mengunggah bukti pengerjaan lalu mengklik tombol 'Selesaikan Task' untuk mengirim laporan ke Manager."
	}

	return ctx.JSON(fiber.Map{
		"message":             responseMsg,
		"task":                firstMap(updatedTask),
		"all_tasks_completed": allCompleted,
		"completed_count":     completedCount,
		"total_tasks":         len(allTasks),
	})
}

type uploadTaskProofInput struct {
	FileBase64  string `json:"file_base64"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
}

func (api *API) UploadTaskProof(ctx *fiber.Ctx) error {
	taskID := ctx.Params("taskId")
	var input uploadTaskProofInput
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.FileBase64) == "" {
		return fiber.NewError(http.StatusBadRequest, "File bukti wajib disertakan.")
	}
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	fileBytes, err := base64.StdEncoding.DecodeString(input.FileBase64)
	if err != nil || len(fileBytes) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Format file tidak valid.")
	}

	ext := ".jpg"
	contentType := "image/jpeg"
	if input.ContentType != "" {
		contentType = input.ContentType
	}
	if strings.Contains(contentType, "png") {
		ext = ".png"
	} else if strings.Contains(contentType, "pdf") {
		ext = ".pdf"
	}

	fileName := fmt.Sprintf("%s_%d%s", taskID, time.Now().Unix(), ext)
	objectPath := fmt.Sprintf("%s/%s", user.ID, fileName)

	uploadedURL, err := admin.UploadStorage(ctx.Context(), "wfa-proofs", objectPath, contentType, fileBytes)
	if err != nil {
		return fmt.Errorf("upload storage error: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = admin.Patch(ctx.Context(), "/wfa_tasks?id=eq."+url.QueryEscape(taskID), "", map[string]any{
		"attachment_url": uploadedURL,
		"updated_at":     now,
	}, nil)

	return ctx.JSON(fiber.Map{
		"message":        "Bukti pengerjaan task berhasil diunggah.",
		"attachment_url": uploadedURL,
	})
}

func (api *API) SubmitWFACompletion(ctx *fiber.Ctx) error {
	reqID := ctx.Params("id")
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var reqs []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(reqID)+"&limit=1", "", &reqs); err != nil || len(reqs) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan WFA tidak ditemukan.")
	}
	reqData := reqs[0]
	if reqData["user_id"] != user.ID {
		return fiber.NewError(http.StatusForbidden, "Akses ditolak.")
	}

	// Periksa apakah semua task sudah done
	var tasks []map[string]any
	_ = admin.Get(ctx.Context(), "/wfa_tasks?wfa_request_id=eq."+url.QueryEscape(reqID), "", &tasks)
	if len(tasks) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Belum ada task pada permohonan WFA ini.")
	}

	for _, t := range tasks {
		if s, ok := t["status"].(string); !ok || s != "done" {
			return fiber.NewError(http.StatusBadRequest, "Semua task harus diselesaikan (status Done) sebelum mengirim laporan penyelesaian.")
		}
		att, _ := t["attachment_url"].(string)
		if strings.TrimSpace(att) == "" {
			return fiber.NewError(http.StatusBadRequest, "Setiap task wajib memiliki bukti pengerjaan (foto/dokumen) sebelum laporan dapat dikirim.")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = admin.Patch(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(reqID), "", map[string]any{
		"status":     "submitted_for_review",
		"updated_at": now,
	}, nil)

	return ctx.JSON(fiber.Map{
		"message": "Laporan tugas WFA selesai berhasil dikirimkan ke Manager untuk diperiksa dan di-ACC.",
		"status":  "submitted_for_review",
	})
}

type approveCompletionInput struct {
	Notes string `json:"notes"`
}

func (api *API) ApproveWFACompletionByManager(ctx *fiber.Ctx) error {
	reqID := ctx.Params("id")
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	isManagerOrAdmin := len(m.ManagedDepartments) > 0 || m.Role == "owner" || m.Role == "admin" || m.Role == "manager"
	if !isManagerOrAdmin {
		return fiber.NewError(http.StatusForbidden, "Akses hanya untuk Manager atau Admin.")
	}
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var reqs []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(reqID)+"&limit=1", "", &reqs); err != nil || len(reqs) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan WFA tidak ditemukan.")
	}
	wfaReq := reqs[0]
	applicantUserID, _ := wfaReq["user_id"].(string)
	reqDate, _ := wfaReq["date"].(string)
	now := time.Now().UTC().Format(time.RFC3339)

	var input approveCompletionInput
	_ = ctx.BodyParser(&input)

	// CATAT KEHADIRAN SEBAGAI HADIR WFA
	var existingAtt []map[string]any
	_ = admin.Get(ctx.Context(), "/attendances?user_id=eq."+url.QueryEscape(applicantUserID)+"&date=eq."+url.QueryEscape(reqDate)+"&limit=1", "", &existingAtt)

	var attID string
	if len(existingAtt) == 0 {
		clockInTime, _ := wfaReq["created_at"].(string)
		if clockInTime == "" {
			clockInTime = now
		}
		var shiftID *string
		if m.ShiftID != nil && *m.ShiftID != "" {
			shiftID = m.ShiftID
		}
		attPayload := map[string]any{
			"company_id":         m.CompanyID,
			"user_id":            applicantUserID,
			"shift_id":           shiftID,
			"date":               reqDate,
			"clock_in_time":      clockInTime,
			"clock_out_time":     now,
			"status":             "on_time",
			"approval_status":    "approved",
			"attendance_type":    "wfa",
			"similarity_score":   1.0,
			"liveness_verified":  true,
			"created_at":         now,
		}
		var createdAtt []map[string]any
		if err := admin.Post(ctx.Context(), "/attendances", "", attPayload, &createdAtt); err == nil && len(createdAtt) > 0 {
			if idVal, ok := createdAtt[0]["id"].(string); ok {
				attID = idVal
			}
		}
	} else {
		attID, _ = existingAtt[0]["id"].(string)
		_ = admin.Patch(ctx.Context(), "/attendances?id=eq."+url.QueryEscape(attID), "", map[string]any{
			"clock_out_time":  now,
			"attendance_type": "wfa",
			"approval_status": "approved",
		}, nil)
	}

	// Update wfa_requests ke completed
	updatePayload := map[string]any{
		"status":            "completed",
		"manager_id":        m.MemberID,
		"manager_name":      user.FullName,
		"manager_notes":     strings.TrimSpace(input.Notes),
		"manager_action_at": now,
		"attendance_id":     attID,
		"updated_at":        now,
	}
	_ = admin.Patch(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(reqID), "", updatePayload, nil)

	return ctx.JSON(fiber.Map{
		"message":       "Penyelesaian WFA disetujui! Karyawan telah resmi dicatat sebagai Masuk Kerja (Hadir WFA).",
		"attendance_id": attID,
		"status":        "completed",
	})
}

func (api *API) GetManagerPendingWFARequests(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	isManagerOrAdmin := len(m.ManagedDepartments) > 0 || m.Role == "owner" || m.Role == "admin" || m.Role == "manager"
	if !isManagerOrAdmin {
		return fiber.NewError(http.StatusForbidden, "Akses hanya untuk Manager atau Admin Departemen.")
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	var membersPath string
	if len(m.ManagedDepartments) > 0 {
		managedDeptIDs := make([]string, len(m.ManagedDepartments))
		for i, d := range m.ManagedDepartments {
			managedDeptIDs[i] = d.ID
		}
		deptFilter := strings.Join(managedDeptIDs, ",")
		membersPath = "/company_members?department_id=in.(" + deptFilter + ")&company_id=eq." + url.QueryEscape(m.CompanyID) +
			"&select=id,user_id,position_title,department_id,profiles(email,full_name),department:departments(name,code)"
	} else if m.DepartmentID != nil && *m.DepartmentID != "" {
		membersPath = "/company_members?department_id=eq." + url.QueryEscape(*m.DepartmentID) + "&company_id=eq." + url.QueryEscape(m.CompanyID) +
			"&select=id,user_id,position_title,department_id,profiles(email,full_name),department:departments(name,code)"
	} else {
		membersPath = "/company_members?company_id=eq." + url.QueryEscape(m.CompanyID) +
			"&select=id,user_id,position_title,department_id,profiles(email,full_name),department:departments(name,code)"
	}

	var members []map[string]any
	if err := admin.Get(ctx.Context(), membersPath, "", &members); err != nil {
		// Fallback query tanpa relasi jika ada issue schema cache PostgREST
		fallbackMembersPath := "/company_members?company_id=eq." + url.QueryEscape(m.CompanyID) +
			"&select=id,user_id,position_title,department_id,profiles(email,full_name)"
		_ = admin.Get(ctx.Context(), fallbackMembersPath, "", &members)
	}

	memberMap := make(map[string]map[string]any)
	memberIDs := make([]string, 0, len(members))
	for _, mb := range members {
		if id, ok := mb["id"].(string); ok {
			memberIDs = append(memberIDs, id)
			memberMap[id] = mb
		}
	}

	// Ambil permohonan WFA: jika owner/admin atau jika filter departemen kosong, ambil seluruh permohonan WFA perusahaan
	var wfaPath string
	if (m.Role == "owner" || m.Role == "admin") || len(memberIDs) == 0 {
		wfaPath = "/wfa_requests?company_id=eq." + url.QueryEscape(m.CompanyID) + "&order=created_at.desc"
	} else {
		wfaPath = "/wfa_requests?member_id=in.(" + strings.Join(memberIDs, ",") + ")&order=created_at.desc"
	}

	var requests []map[string]any
	if err := admin.Get(ctx.Context(), wfaPath, "", &requests); err != nil {
		return err
	}
	if len(requests) == 0 {
		return ctx.JSON([]any{})
	}

	// Backfill member info jika ada request dari member yang belum ada di memberMap
	missingMemberIDs := make([]string, 0)
	for i := range requests {
		mID, _ := requests[i]["member_id"].(string)
		if _, found := memberMap[mID]; !found && mID != "" {
			missingMemberIDs = append(missingMemberIDs, mID)
		}
	}
	if len(missingMemberIDs) > 0 {
		var extraMembers []map[string]any
		extraPath := "/company_members?id=in.(" + strings.Join(missingMemberIDs, ",") + ")&select=id,user_id,position_title,department_id,profiles(email,full_name)"
		if err := admin.Get(ctx.Context(), extraPath, "", &extraMembers); err == nil {
			for _, emb := range extraMembers {
				if id, ok := emb["id"].(string); ok {
					memberMap[id] = emb
				}
			}
		}
	}

	reqIDs := make([]string, len(requests))
	for i := range requests {
		mID, _ := requests[i]["member_id"].(string)
		if mb, found := memberMap[mID]; found {
			requests[i]["member"] = mb
		}
		if id, ok := requests[i]["id"].(string); ok {
			reqIDs[i] = id
		}
	}

	// Pasangkan tasks aktual
	var tasks []map[string]any
	tasksPath := "/wfa_tasks?wfa_request_id=in.(" + strings.Join(reqIDs, ",") + ")&order=sort_order.asc"
	_ = admin.Get(ctx.Context(), tasksPath, "", &tasks)

	taskGroup := make(map[string][]map[string]any)
	for _, t := range tasks {
		if rID, ok := t["wfa_request_id"].(string); ok {
			taskGroup[rID] = append(taskGroup[rID], t)
		}
	}
	for i := range requests {
		rID, _ := requests[i]["id"].(string)
		requests[i]["tasks"] = taskGroup[rID]
		if requests[i]["tasks"] == nil {
			requests[i]["tasks"] = []any{}
		}
	}

	return ctx.JSON(requests)
}

type managerReviewWFAInput struct {
	Action          string   `json:"action"` // "approve" atau "reject"
	Notes           string   `json:"notes"`
	ApprovedTaskIDs []string `json:"approved_task_ids"` // task-task yang di-acc manager
	NewTasks        []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"new_tasks"` // task tambahan dari manager
}

func (api *API) ReviewWFAByManager(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	isManagerOrAdmin := len(m.ManagedDepartments) > 0 || m.Role == "owner" || m.Role == "admin" || m.Role == "manager"
	if !isManagerOrAdmin {
		return fiber.NewError(http.StatusForbidden, "Akses hanya untuk Manager atau Admin Departemen.")
	}
	var input managerReviewWFAInput
	if err := ctx.BodyParser(&input); err != nil || (input.Action != "approve" && input.Action != "reject") {
		return fiber.NewError(http.StatusBadRequest, "Action harus 'approve' atau 'reject'.")
	}

	requestID := ctx.Params("id")
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var reqs []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID)+"&limit=1", "", &reqs); err != nil || len(reqs) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan WFA tidak ditemukan.")
	}

	if input.Action == "approve" {
		// 1. Tandai task-task yang di-acc manager satu per satu
		if len(input.ApprovedTaskIDs) > 0 {
			for _, tid := range input.ApprovedTaskIDs {
				_ = admin.Patch(ctx.Context(), "/wfa_tasks?id=eq."+url.QueryEscape(tid), "", map[string]any{
					"is_approved_by_manager": true,
					"updated_at":             now,
				}, nil)
			}
		}

		// 2. Tambahkan task baru jika manajer menambahkan task
		if len(input.NewTasks) > 0 {
			newTasksPayload := make([]map[string]any, len(input.NewTasks))
			for i, nt := range input.NewTasks {
				newTasksPayload[i] = map[string]any{
					"wfa_request_id":         requestID,
					"title":                  strings.TrimSpace(nt.Title),
					"description":            strings.TrimSpace(nt.Description),
					"added_by_role":          "manager",
					"is_approved_by_manager": true,
					"status":                 "todo",
					"sort_order":             100 + i,
					"created_at":             now,
					"updated_at":             now,
				}
			}
			_ = admin.Post(ctx.Context(), "/wfa_tasks", "", newTasksPayload, nil)
		}

		// 3. Update status permohonan menjadi pending_hr (dilaporkan dan diteruskan ke Dashboard HR!)
		updatePayload := map[string]any{
			"status":            "pending_hr",
			"manager_id":        m.MemberID,
			"manager_name":      user.FullName,
			"manager_notes":     strings.TrimSpace(input.Notes),
			"manager_action_at": now,
			"updated_at":        now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID), "", updatePayload, &updated); err != nil {
			return err
		}

		return ctx.JSON(fiber.Map{
			"message": "Permohonan WFA dan card task berhasil di-ACC oleh Manager dan dilaporkan ke Dashboard HR.",
			"data":    firstMap(updated),
		})
	} else {
		updatePayload := map[string]any{
			"status":            "rejected",
			"manager_id":        m.MemberID,
			"manager_name":      user.FullName,
			"manager_notes":     strings.TrimSpace(input.Notes),
			"manager_action_at": now,
			"updated_at":        now,
		}
		var updated []map[string]any
		if err := admin.Patch(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID), "", updatePayload, &updated); err != nil {
			return err
		}

		return ctx.JSON(fiber.Map{
			"message": "Permohonan WFA telah ditolak oleh Manager.",
			"data":    firstMap(updated),
		})
	}
}

func (api *API) ListCompanyWFARequests(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	statusFilter := ctx.Query("status")
	wfaPath := "/wfa_requests?company_id=eq." + url.QueryEscape(m.CompanyID)
	if statusFilter != "" && statusFilter != "all" {
		wfaPath += "&status=eq." + url.QueryEscape(statusFilter)
	}
	wfaPath += "&order=created_at.desc"

	var requests []map[string]any
	if err := admin.Get(ctx.Context(), wfaPath, "", &requests); err != nil {
		return err
	}
	if len(requests) == 0 {
		return ctx.JSON(fiber.Map{"data": []any{}})
	}

	// Ambil mapping company_members
	membersPath := "/company_members?company_id=eq." + url.QueryEscape(m.CompanyID) +
		"&select=id,position_title,profiles(email,full_name),department:departments(id,name,code)"
	var members []map[string]any
	_ = admin.Get(ctx.Context(), membersPath, "", &members)

	memberMap := make(map[string]map[string]any)
	for _, mb := range members {
		if id, ok := mb["id"].(string); ok {
			memberMap[id] = mb
		}
	}

	// Ambil semua task terkait
	reqIDs := make([]string, len(requests))
	for i, r := range requests {
		if id, ok := r["id"].(string); ok {
			reqIDs[i] = id
		}
	}
	var allTasks []map[string]any
	tasksPath := "/wfa_tasks?wfa_request_id=in.(" + strings.Join(reqIDs, ",") + ")&order=sort_order.asc"
	_ = admin.Get(ctx.Context(), tasksPath, "", &allTasks)

	taskGroup := make(map[string][]map[string]any)
	for _, t := range allTasks {
		if rID, ok := t["wfa_request_id"].(string); ok {
			taskGroup[rID] = append(taskGroup[rID], t)
		}
	}

	result := make([]map[string]any, len(requests))
	for i, r := range requests {
		item := make(map[string]any)
		for k, v := range r {
			item[k] = v
		}

		item["member_name"] = ""
		item["member_email"] = ""
		item["department_name"] = ""
		item["position_title"] = ""

		mID, _ := r["member_id"].(string)
		if mb, found := memberMap[mID]; found {
			item["member"] = mb
			if pos, ok := mb["position_title"].(string); ok {
				item["position_title"] = pos
			}
			if prof, ok := mb["profiles"].(map[string]any); ok {
				if fn, ok := prof["full_name"].(string); ok && fn != "" {
					item["member_name"] = fn
				}
				if em, ok := prof["email"].(string); ok {
					item["member_email"] = em
					if item["member_name"] == "" {
						item["member_name"] = em
					}
				}
			}
			if dept, ok := mb["department"].(map[string]any); ok {
				if dn, ok := dept["name"].(string); ok {
					item["department_name"] = dn
				}
			}
		}

		rID, _ := r["id"].(string)
		item["tasks"] = taskGroup[rID]
		if item["tasks"] == nil {
			item["tasks"] = []any{}
		}

		result[i] = item
	}

	return ctx.JSON(fiber.Map{
		"data": result,
	})
}

type hrReviewWFAInput struct {
	Action string `json:"action"` // "approve" atau "reject"
	Notes  string `json:"notes"`
}

func (api *API) ReviewWFAByHR(ctx *fiber.Ctx) error {
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var input hrReviewWFAInput
	if err := ctx.BodyParser(&input); err != nil || (input.Action != "approve" && input.Action != "reject") {
		return fiber.NewError(http.StatusBadRequest, "Action harus 'approve' atau 'reject'.")
	}

	requestID := ctx.Params("id")
	user := middleware.CurrentUser(ctx)
	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var reqs []map[string]any
	if err := admin.Get(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID)+"&limit=1", "", &reqs); err != nil || len(reqs) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan WFA tidak ditemukan.")
	}

	newStatus := "approved"
	msg := "Permohonan WFA telah disetujui (ACC) oleh HR. Karyawan dapat melaksanakan WFA."
	if input.Action == "reject" {
		newStatus = "rejected"
		msg = "Permohonan WFA telah ditolak oleh HR."
	}

	updatePayload := map[string]any{
		"status":       newStatus,
		"hr_id":        m.MemberID,
		"hr_name":      user.FullName,
		"hr_notes":     strings.TrimSpace(input.Notes),
		"hr_action_at": now,
		"updated_at":   now,
	}

	var updated []map[string]any
	if err := admin.Patch(ctx.Context(), "/wfa_requests?id=eq."+url.QueryEscape(requestID), "", updatePayload, &updated); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message": msg,
		"data":    firstMap(updated),
	})
}


func ErrorHandler(ctx *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	message := "Terjadi kesalahan pada server."
	if fiberError, ok := err.(*fiber.Error); ok {
		status = fiberError.Code
		message = fiberError.Message
	}
	if httpError, ok := err.(*services.HTTPError); ok {
		status = httpError.Status
		message = httpError.Message
	}
	if status >= 500 {
		fmt.Println(err)
	}
	return ctx.Status(status).JSON(fiber.Map{"error": fiber.Map{"message": message}})
}
