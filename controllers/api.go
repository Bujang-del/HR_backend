package controllers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"hr-gateway-backend/middleware"
	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
)

type API struct {
	Supabase          *services.SupabaseClient
	SupabaseAdmin     *services.SupabaseClient
	InviteRedirectURL string
}

type membership struct {
	CompanyID     string  `json:"company_id"`
	Role          string  `json:"role"`
	PositionTitle *string `json:"position_title"`
}

type profile struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type memberInput struct {
	Email         string  `json:"email"`
	Position      string  `json:"position"`
	PositionTitle string  `json:"position_title"`
	Role          string  `json:"role"`
	DepartmentID  *string `json:"department_id"`
}

type importRow struct {
	Email      string `json:"email"`
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

func (api *API) Me(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	return ctx.JSON(fiber.Map{"user": middleware.CurrentUser(ctx), "membership": membership})
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
	if err := api.Supabase.Get(ctx.Context(), path, middleware.AccessToken(ctx), &members); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(members))
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
	userID, _, err := api.findOrInviteUser(ctx, email)
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
		userID, invited, findErr := api.findOrInviteUser(ctx, row.Email)
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

func (api *API) findOrInviteUser(ctx *fiber.Ctx, email string) (string, bool, error) {
	if api.SupabaseAdmin == nil {
		return "", false, fiber.NewError(http.StatusInternalServerError, "Supabase Admin API belum dikonfigurasi.")
	}
	var profiles []profile
	profilePath := "/profiles?email=eq." + url.QueryEscape(email) + "&select=id,email&limit=1"
	if err := api.SupabaseAdmin.Get(ctx.Context(), profilePath, "", &profiles); err != nil {
		return "", false, err
	}
	if len(profiles) > 0 {
		return profiles[0].ID, false, nil
	}
	var invitedUser services.AuthUser
	if err := api.SupabaseAdmin.InviteUser(ctx.Context(), email, api.InviteRedirectURL, &invitedUser); err != nil {
		return "", false, err
	}
	if invitedUser.ID == "" {
		return "", false, fiber.NewError(http.StatusBadGateway, "Supabase tidak mengembalikan user hasil undangan.")
	}
	return invitedUser.ID, true, nil
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

func (api *API) ListDepartments(ctx *fiber.Ctx) error {
	membership, err := api.currentMembership(ctx)
	if err != nil {
		return err
	}
	var departments []map[string]any
	path := "/departments?company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,name,code,manager_id,members:company_members!company_members_department_id_fkey(id,user_id,role,position_title,department_id,profiles(email,full_name))&order=name.asc"
	if err := api.Supabase.Get(ctx.Context(), path, middleware.AccessToken(ctx), &departments); err != nil {
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
	if err := api.Supabase.Post(ctx.Context(), "/departments?select=id,name,code,manager_id", middleware.AccessToken(ctx), payload, &departments); err != nil {
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
	if err := api.Supabase.Get(ctx.Context(), path, middleware.AccessToken(ctx), &departments); err != nil {
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
	path := "/departments?id=eq." + url.QueryEscape(ctx.Params("id")) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,name,code,manager_id"
	var departments []map[string]any
	if err := api.Supabase.Patch(ctx.Context(), path, middleware.AccessToken(ctx), fiber.Map{"manager_id": input.ManagerID}, &departments); err != nil {
		return err
	}
	return ctx.JSON(firstMap(departments))
}

func (api *API) AssignMemberDepartment(ctx *fiber.Ctx) error {
	membership, err := api.requireHR(ctx)
	if err != nil {
		return err
	}
	var input struct {
		DepartmentID *string `json:"department_id"`
	}
	if err := ctx.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	path := "/company_members?id=eq." + url.QueryEscape(ctx.Params("id")) + "&company_id=eq." + url.QueryEscape(membership.CompanyID) + "&select=id,user_id,role,position_title,department_id,profiles(email,full_name)"
	var members []map[string]any
	if err := api.Supabase.Patch(ctx.Context(), path, middleware.AccessToken(ctx), input, &members); err != nil {
		return err
	}
	return ctx.JSON(normalizeProfiles(firstMap(members)))
}

func (api *API) currentMembership(ctx *fiber.Ctx) (membership, error) {
	user := middleware.CurrentUser(ctx)
	var memberships []membership
	path := "/company_members?user_id=eq." + url.QueryEscape(user.ID) + "&select=company_id,role,position_title&order=joined_at.asc&limit=1"
	if err := api.Supabase.Get(ctx.Context(), path, middleware.AccessToken(ctx), &memberships); err != nil {
		return membership{}, err
	}
	if len(memberships) == 0 {
		return membership{}, fiber.NewError(http.StatusNotFound, "Perusahaan belum ditemukan.")
	}
	return memberships[0], nil
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
					item[key] = []any{profileMap}
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
