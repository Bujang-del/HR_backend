package middleware

import (
	"net/url"
	"strings"

	"hr-gateway-backend/services"

	"github.com/gofiber/fiber/v2"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func RequireAuth(client *services.SupabaseClient) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		header := ctx.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(header, "Bearer ") {
			return fiber.ErrUnauthorized
		}
		accessToken := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if accessToken == "" {
			return fiber.ErrUnauthorized
		}

		var user User
		if err := client.AuthUser(ctx.Context(), accessToken, &user); err != nil {
			if httpError, ok := err.(*services.HTTPError); ok && (httpError.Status == fiber.StatusUnauthorized || httpError.Status == fiber.StatusForbidden) {
				return fiber.ErrUnauthorized
			}
			return fiber.NewError(fiber.StatusServiceUnavailable, "Supabase tidak dapat dihubungi.")
		}
		ctx.Locals("access_token", accessToken)
		ctx.Locals("user", user)
		return ctx.Next()
	}
}

func AccessToken(ctx *fiber.Ctx) string {
	value, _ := ctx.Locals("access_token").(string)
	return value
}

func CurrentUser(ctx *fiber.Ctx) User {
	user, _ := ctx.Locals("user").(User)
	return user
}

func RequireHR(client *services.SupabaseClient) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		user := CurrentUser(ctx)
		var memberships []struct {
			ID string `json:"id"`
		}
		path := "/company_members?user_id=eq." + url.QueryEscape(user.ID) + "&role=eq.hr&select=id&limit=1"
		if err := client.Get(ctx.Context(), path, AccessToken(ctx), &memberships); err != nil {
			return err
		}
		if len(memberships) == 0 {
			return fiber.ErrForbidden
		}
		return ctx.Next()
	}
}
