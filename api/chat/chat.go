// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package chat

import (
	"context"

	v1 "SentinelOps/api/chat/v1"
)

type IChatV1 interface {
	FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error)
	Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error)
}
