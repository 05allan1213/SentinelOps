// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package chat

import (
	"context"

	v1 "SentinelOps/api/chat/v1"
	v2 "SentinelOps/api/chat/v2"
)

type IChatV1 interface {
	FileUpload(ctx context.Context, req *v1.FileUploadReq) (res *v1.FileUploadRes, err error)
	Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error)
}

type IChatV2 interface {
	CreateRun(ctx context.Context, req *v2.CreateRunReq) (res *v2.CreateRunRes, err error)
	RunEvents(ctx context.Context, req *v2.RunEventsReq) (res *v2.RunEventsRes, err error)
}
