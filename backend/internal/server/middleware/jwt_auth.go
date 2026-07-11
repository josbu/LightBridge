package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/WilliamWang1721/LightBridge/internal/service"

	"github.com/gin-gonic/gin"
)

// NewJWTAuthMiddleware 创建 JWT 认证中间件
func NewJWTAuthMiddleware(authService *service.AuthService, userService *service.UserService) JWTAuthMiddleware {
	return JWTAuthMiddleware(jwtAuth(authService, userService, userService))
}

type jwtUserReader interface {
	GetByID(ctx context.Context, id int64) (*service.User, error)
}

type userActivityToucher interface {
	TouchLastActiveForUser(ctx context.Context, user *service.User)
}

// jwtAuth JWT认证中间件实现
func jwtAuth(authService *service.AuthService, userService jwtUserReader, activityToucher userActivityToucher) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从Authorization header中提取token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			AbortWithError(c, 401, "UNAUTHORIZED", "Authorization header is required")
			return
		}

		// 验证Bearer scheme
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			AbortWithError(c, 401, "INVALID_AUTH_HEADER", "Authorization header format must be 'Bearer {token}'")
			return
		}

		tokenString := strings.TrimSpace(parts[1])
		if tokenString == "" {
			AbortWithError(c, 401, "EMPTY_TOKEN", "Token cannot be empty")
			return
		}

		// 验证token
		claims, err := authService.ValidateToken(tokenString)
		if err != nil {
			if errors.Is(err, service.ErrTokenExpired) {
				AbortWithError(c, 401, "TOKEN_EXPIRED", "Token has expired")
				return
			}
			AbortWithError(c, 401, "INVALID_TOKEN", "Invalid token")
			return
		}

		if !validateJWTRequestScope(c, claims) {
			return
		}

		// 从数据库获取最新的用户信息
		user, err := userService.GetByID(c.Request.Context(), claims.UserID)
		if err != nil {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
			return
		}

		// 检查用户状态
		if !user.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}

		// Security: Validate TokenVersion to ensure token hasn't been invalidated
		// This check ensures tokens issued before a password change are rejected
		if claims.TokenVersion != user.TokenVersion {
			AbortWithError(c, 401, "TOKEN_REVOKED", "Token has been revoked (password changed)")
			return
		}

		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      user.ID,
			Concurrency: user.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), user.Role)
		if activityToucher != nil {
			activityToucher.TouchLastActiveForUser(c.Request.Context(), user)
		}

		c.Next()
	}
}

func validateJWTRequestScope(c *gin.Context, claims *service.JWTClaims) bool {
	if claims == nil || strings.TrimSpace(claims.Scope) == "" {
		return true
	}
	if claims.Scope != service.JWTTokenScopePaymentEmbed {
		AbortWithError(c, 403, "TOKEN_SCOPE_FORBIDDEN", "Token scope is not allowed")
		return false
	}

	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	if path != "/api/v1/payment" && !strings.HasPrefix(path, "/api/v1/payment/") {
		AbortWithError(c, 403, "TOKEN_SCOPE_FORBIDDEN", "Embedded payment token is restricted to payment APIs")
		return false
	}

	origin := ""
	if c.Request != nil {
		origin = strings.TrimSpace(c.Request.Header.Get("Origin"))
	}
	if origin == "" || !jwtAudienceContains(claims, origin) {
		AbortWithError(c, 403, "TOKEN_AUDIENCE_MISMATCH", "Embedded payment token origin does not match")
		return false
	}
	return true
}

func jwtAudienceContains(claims *service.JWTClaims, audience string) bool {
	if claims == nil || audience == "" {
		return false
	}
	for _, candidate := range claims.Audience {
		if candidate == audience {
			return true
		}
	}
	return false
}

// Deprecated: prefer GetAuthSubjectFromContext in auth_subject.go.
