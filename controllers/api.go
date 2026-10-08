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
	return ctx.JSON(fiber.Map{"status": "ok"})
}

func (api *API) Login(ctx *fiber.Ctx) error {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := ctx.BodyParser(&input); err != nil || strings.TrimSpace(input.Email) == "" || input.Password == "" {
		return fiber.ErrBadRequest
	}
	var session map[string]any
	if err := api.Supabase.PasswordLogin(ctx.Context(), strings.ToLower(strings.TrimSpace(input.Email)), input.Password, &session); err != nil {
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
	// Contoh: "Manager" + " " + "SDM" → "Manager SDM"
	//         "Staff"   + " " + "FI"  → "Staff FI"
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
	if api.SupabaseAdmin != nil {
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
	// managed_departments: join balik dari departments.manager_id → company_members.id
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
			"is_enrolled":    false,
			"face_photo_url": "",
			"face_embedding": []any{},
		})
	}
	return ctx.JSON(fiber.Map{
		"is_enrolled":    true,
		"face_photo_url": records[0]["face_photo_url"],
		"face_embedding": records[0]["face_embedding"],
	})
}

func (api *API) EnrollMyFace(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	var body struct {
		FaceEmbedding []float64 `json:"face_embedding"`
		PhotoBase64   string    `json:"photo_base64"`
	}
	if err := ctx.BodyParser(&body); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Payload pendaftaran wajah tidak valid.")
	}
	if len(body.FaceEmbedding) == 0 {
		return fiber.NewError(http.StatusBadRequest, "Fitur embedding wajah tidak boleh kosong.")
	}

	admin := api.SupabaseAdmin
	if admin == nil {
		admin = api.Supabase
	}

	photoURL := ""
	if body.PhotoBase64 != "" {
		photoBytes, err := base64.StdEncoding.DecodeString(body.PhotoBase64)
		if err == nil && len(photoBytes) > 0 {
			objectPath := fmt.Sprintf("%s/master.jpg", user.ID)
			uploadedURL, uploadErr := admin.UploadStorage(ctx.Context(), "biometric-faces", objectPath, "image/jpeg", photoBytes)
			if uploadErr == nil {
				photoURL = uploadedURL
			}
		}
	}

	path := "/user_biometrics?user_id=eq." + url.QueryEscape(user.ID) + "&limit=1"
	var existing []map[string]any
	_ = admin.Get(ctx.Context(), path, "", &existing)

	payload := map[string]any{
		"user_id":        user.ID,
		"face_embedding": body.FaceEmbedding,
		"updated_at":     time.Now().UTC().Format(time.RFC3339),
	}
	if photoURL != "" {
		payload["face_photo_url"] = photoURL
	}

	if len(existing) > 0 {
		if err := admin.Patch(ctx.Context(), path, "", payload, nil); err != nil {
			return err
		}
	} else {
		payload["created_at"] = time.Now().UTC().Format(time.RFC3339)
		if err := admin.Post(ctx.Context(), "/user_biometrics", "", payload, nil); err != nil {
			return err
		}
	}

	return ctx.JSON(fiber.Map{
		"message":        "Wajah biometrik berhasil didaftarkan.",
		"is_enrolled":    true,
		"face_photo_url": photoURL,
	})
}

func (api *API) ClockIn(ctx *fiber.Ctx) error {
	user := middleware.CurrentUser(ctx)
	m, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}

	var body struct {
		SimilarityScore  float64 `json:"similarity_score"`
		LivenessVerified bool    `json:"liveness_verified"`
		PhotoBase64      string  `json:"photo_base64"`
	}
	if err := ctx.BodyParser(&body); err != nil {
		return fiber.NewError(http.StatusBadRequest, "Payload clock-in tidak valid.")
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
		"approval_status":    "pending",
		"similarity_score":   body.SimilarityScore,
		"liveness_verified":  body.LivenessVerified,
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
			"message":            "Clock In ulang berhasil. Menunggu verifikasi HR.",
			"status":             status,
			"clock_in_time":      now.Format("15:04:05"),
			"clock_in_photo_url": photoURL,
		})
	}

	var result []map[string]any
	if err := admin.Post(ctx.Context(), "/attendances", "", payload, &result); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message":            "Clock In berhasil.",
		"status":             status,
		"clock_in_time":      now.Format("15:04:05"),
		"clock_in_photo_url": photoURL,
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
		SimilarityScore  float64 `json:"similarity_score"`
		LivenessVerified bool    `json:"liveness_verified"`
		PhotoBase64      string  `json:"photo_base64"`
	}
	_ = ctx.BodyParser(&body)

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
		"similarity_score":  body.SimilarityScore,
		"liveness_verified": body.LivenessVerified,
	}
	if photoURL != "" {
		payload["clock_out_photo_url"] = photoURL
	}

	if err := admin.Patch(ctx.Context(), checkPath, "", payload, nil); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message":             "Clock Out berhasil.",
		"clock_out_time":      now.Format("15:04:05"),
		"clock_out_photo_url": photoURL,
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

	path := fmt.Sprintf("/attendances?user_id=eq.%s&order=date.desc&limit=14", url.QueryEscape(user.ID))
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

	// Check if already has a pending request
	checkPath := fmt.Sprintf("/face_reset_requests?user_id=eq.%s&status=eq.pending&limit=1", url.QueryEscape(user.ID))
	var pending []map[string]any
	if err := admin.Get(ctx.Context(), checkPath, "", &pending); err == nil && len(pending) > 0 {
		return fiber.NewError(http.StatusBadRequest, "Anda sudah memiliki permohonan yang sedang menunggu persetujuan HR.")
	}

	payload := map[string]any{
		"company_id": m.CompanyID,
		"user_id":    user.ID,
		"reason":     strings.TrimSpace(body.Reason),
		"status":     "pending",
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}

	if err := admin.Post(ctx.Context(), "/face_reset_requests", "", payload, nil); err != nil {
		return err
	}

	return ctx.JSON(fiber.Map{
		"message": "Permohonan daftar ulang wajah berhasil dikirim ke HR.",
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

	// Enrich with employee profile information
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

	// Get the request
	path := fmt.Sprintf("/face_reset_requests?id=eq.%s&limit=1", url.QueryEscape(id))
	var requests []map[string]any
	if err := admin.Get(ctx.Context(), path, "", &requests); err != nil || len(requests) == 0 {
		return fiber.NewError(http.StatusNotFound, "Permohonan tidak ditemukan.")
	}

	targetUserID, _ := requests[0]["user_id"].(string)

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

	// If approved, delete the old biometrics data so employee can enroll afresh!
	if body.Action == "approved" && targetUserID != "" {
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
