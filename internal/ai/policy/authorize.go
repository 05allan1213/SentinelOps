// Package policy 提供服务端 Identity、Scope 与 RBAC 授权基元。
package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DisabledUserID 是认证关闭时固定注入的只读用户 ID。
const DisabledUserID = "auth-disabled-viewer"

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
)

// Role 是服务端规范化后的用户角色。
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleApprover Role = "approver"
	RoleAdmin    Role = "admin"
)

// Scope 描述当前身份可访问的用户范围。
type Scope struct {
	UserID string
	All    bool
}

// Identity 是由认证中间件写入 Context 的唯一服务端身份。
type Identity struct {
	UserID       string
	Username     string
	Role         Role
	Scope        Scope
	AuthDisabled bool
}

// Permission 是 P05 冻结的授权能力。
type Permission string

const (
	PermissionViewScoped             Permission = "view_scoped"
	PermissionCreateReadOnlyRun      Permission = "create_read_only_run"
	PermissionWriteOwnFeedback       Permission = "write_own_feedback"
	PermissionProposeMutation        Permission = "propose_mutation"
	PermissionExecutePreauthorizedL1 Permission = "execute_preauthorized_l1"
	PermissionDecideProposal         Permission = "decide_proposal"
	PermissionBusinessWrite          Permission = "business_write"
	PermissionManageUsersPolicyGates Permission = "manage_users_policy_gates"
)

// Resource 携带资源级授权所需的最小事实。
type Resource struct {
	OwnerID         string
	Preauthorized   bool
	StaticL1Allowed bool
}

type identityContextKey struct{}

// NormalizeRole 将旧角色映射到 P05 的规范角色集合。
func NormalizeRole(role string) (Role, error) {
	switch Role(strings.ToLower(strings.TrimSpace(role))) {
	case "user", RoleViewer:
		return RoleViewer, nil
	case RoleOperator:
		return RoleOperator, nil
	case RoleApprover:
		return RoleApprover, nil
	case RoleAdmin:
		return RoleAdmin, nil
	default:
		return "", fmt.Errorf("unknown role %q: %w", role, ErrForbidden)
	}
}

// NewIdentity 校验认证结果并构造默认 Scope。
func NewIdentity(userID, username, role string) (Identity, error) {
	if strings.TrimSpace(userID) == "" {
		return Identity{}, ErrUnauthenticated
	}
	normalizedRole, err := NormalizeRole(role)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{
		UserID:   userID,
		Username: username,
		Role:     normalizedRole,
		Scope:    Scope{UserID: userID},
	}
	if normalizedRole == RoleAdmin {
		identity.Scope.All = true
	}
	return identity, nil
}

// DisabledIdentity 返回认证关闭时唯一允许注入的只读 viewer。
func DisabledIdentity() Identity {
	return Identity{
		UserID:       DisabledUserID,
		Role:         RoleViewer,
		Scope:        Scope{UserID: DisabledUserID},
		AuthDisabled: true,
	}
}

// WithIdentity 将服务端身份写入 Context。
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// IdentityFromContext 读取并校验服务端身份。
func IdentityFromContext(ctx context.Context) (Identity, error) {
	identity, ok := ctx.Value(identityContextKey{}).(Identity)
	if !ok || identity.UserID == "" {
		return Identity{}, ErrUnauthenticated
	}
	role, err := NormalizeRole(string(identity.Role))
	if err != nil {
		return Identity{}, err
	}
	identity.Role = role
	if identity.AuthDisabled {
		if identity.UserID != DisabledUserID || identity.Role != RoleViewer || identity.Scope.All || identity.Scope.UserID != DisabledUserID {
			return Identity{}, ErrForbidden
		}
		return identity, nil
	}
	if identity.Scope.All {
		if identity.Role != RoleAdmin {
			return Identity{}, ErrForbidden
		}
	} else if identity.Scope.UserID != identity.UserID {
		return Identity{}, ErrForbidden
	}
	return identity, nil
}

// UserID 返回 Context 中的服务端用户 ID，不接受客户端 fallback。
func UserID(ctx context.Context) (string, error) {
	identity, err := IdentityFromContext(ctx)
	if err != nil {
		return "", err
	}
	return identity.UserID, nil
}

// Authorize 根据 typed Identity、Permission 与资源事实执行 fail-closed 授权。
func Authorize(ctx context.Context, permission Permission, resource Resource) error {
	identity, err := IdentityFromContext(ctx)
	if err != nil {
		return err
	}

	if identity.AuthDisabled && permission != PermissionViewScoped && permission != PermissionCreateReadOnlyRun {
		return ErrForbidden
	}

	switch permission {
	case PermissionViewScoped:
		if resource.OwnerID == "" || identity.Scope.All || resource.OwnerID == identity.Scope.UserID {
			return nil
		}
	case PermissionCreateReadOnlyRun:
		return nil
	case PermissionWriteOwnFeedback:
		if !identity.AuthDisabled && canMutate(identity.Role) && (resource.OwnerID == "" || resource.OwnerID == identity.UserID) {
			return nil
		}
	case PermissionProposeMutation, PermissionBusinessWrite:
		if canMutate(identity.Role) {
			return nil
		}
	case PermissionExecutePreauthorizedL1:
		if canMutate(identity.Role) && resource.Preauthorized && resource.StaticL1Allowed {
			return nil
		}
	case PermissionDecideProposal:
		if (identity.Role == RoleApprover || identity.Role == RoleAdmin) && (resource.OwnerID == "" || resource.OwnerID != identity.UserID) {
			return nil
		}
	case PermissionManageUsersPolicyGates:
		if identity.Role == RoleAdmin {
			return nil
		}
	default:
		return fmt.Errorf("unknown permission %q: %w", permission, ErrForbidden)
	}
	return ErrForbidden
}

func canMutate(role Role) bool {
	return role == RoleOperator || role == RoleApprover || role == RoleAdmin
}
