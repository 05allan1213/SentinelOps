package runtime

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	runtimev1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
	service "SentinelOps/internal/service/runtime"

	"gorm.io/gorm"
)

func TestRuntimeHTTPErrorStable(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrRuntimeNotFound, 404, service.ErrorCodeRuntimeNotFound},
		{service.ErrRuntimeForbidden, 403, service.ErrorCodeRuntimeForbidden},
		{service.ErrRuntimeOperationConflict, 409, service.ErrorCodeRuntimeOperationConflict},
		{service.ErrRuntimeIdempotencyConflict, 409, service.ErrorCodeRuntimeIdempotencyConflict},
		{service.ErrRuntimePrecondition, 422, service.ErrorCodeRuntimePreconditionFailed},
		{service.ErrRuntimeInvalidFilter, 400, service.ErrorCodeRuntimeInvalidFilter},
		{errors.New("password=super-secret"), 500, service.ErrorCodeRuntimeInternal},
		{gorm.ErrRecordNotFound, 404, service.ErrorCodeRuntimeNotFound},
		{policy.ErrForbidden, 403, service.ErrorCodeRuntimeForbidden},
		{policy.ErrUnauthenticated, 403, service.ErrorCodeRuntimeForbidden},
		{errors.Join(errors.New("wrapped"), gorm.ErrRecordNotFound), 404, service.ErrorCodeRuntimeNotFound},
		{errors.Join(errors.New("wrapped"), service.ErrRuntimeOperationConflict), 409, service.ErrorCodeRuntimeOperationConflict},
		{runtimev1.ErrRuntimeRequestValidation, 400, service.ErrorCodeRuntimeInvalidFilter},
		{fmt.Errorf("wrapped validation: %w", runtimev1.ErrRuntimeRequestValidation), 400, service.ErrorCodeRuntimeInvalidFilter},
	}
	for _, tt := range tests {
		got := service.RuntimeHTTPError(tt.err)
		if got.Status != tt.status || got.Code != tt.code || got.Message == "" || strings.Contains(got.Message, "super-secret") {
			t.Errorf("%#v", got)
		}
	}
}
