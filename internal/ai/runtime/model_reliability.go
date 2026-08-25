package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"SentinelOps/internal/ai/models"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const modelReservationDomain = "sentinelops/model-call-reservation/v1\x00"

// PhysicalModelBinder 把唯一 RuntimeHandler 绑定到每个 Eino Failover 候选。
func PhysicalModelBinder(handler *RuntimeHandler) models.PhysicalModelBinder {
	return func(identity models.CandidateIdentity, endpoint model.BaseChatModel) (model.BaseChatModel, error) {
		if handler == nil || endpoint == nil {
			return nil, fmt.Errorf("runtime Handler and physical Model endpoint are required")
		}
		if identity.CatalogRef == "" || identity.Provider == "" || identity.Driver == "" || identity.ModelID == "" || identity.Profile == "" {
			return nil, fmt.Errorf("physical Model candidate identity is incomplete")
		}
		return &boundPhysicalModel{handler: handler, endpoint: endpoint, identity: identity}, nil
	}
}

type boundPhysicalModel struct {
	handler  *RuntimeHandler
	endpoint model.BaseChatModel
	identity models.CandidateIdentity
}

func (m *boundPhysicalModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	callContext, err := m.callContext(ctx)
	if err != nil {
		return nil, err
	}
	return (&runtimeModelEndpoint{handler: m.handler, endpoint: m.endpoint}).Generate(callContext, input, options...)
}

func (m *boundPhysicalModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	callContext, err := m.callContext(ctx)
	if err != nil {
		return nil, err
	}
	return (&runtimeModelEndpoint{handler: m.handler, endpoint: m.endpoint}).Stream(callContext, input, options...)
}

func (m *boundPhysicalModel) callContext(ctx context.Context) (context.Context, error) {
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if attempt.physicalCalls == nil {
		return nil, fmt.Errorf("physical Model call sequence is missing")
	}
	var frozen *ModelSnapshot
	for _, candidate := range attempt.Snapshot.Models() {
		if candidate.Kind == "chat" && candidate.Profile == m.identity.Profile && candidate.CandidateOrder == m.identity.Order &&
			candidate.CatalogRef == m.identity.CatalogRef && candidate.Provider == m.identity.Provider &&
			candidate.Driver == m.identity.Driver && candidate.ModelID == m.identity.ModelID {
			copy := candidate
			frozen = &copy
			break
		}
	}
	if frozen == nil {
		return nil, fmt.Errorf("physical Model candidate does not match Frozen Runtime Snapshot")
	}
	ordinal := attempt.physicalCalls.Add(1)
	invocation := ModelInvocation{
		ReservationIdentity: modelReservationIdentity(attempt, ordinal, frozen.CatalogRef),
		CatalogRef:          frozen.CatalogRef, Provider: frozen.Provider, Driver: frozen.Driver,
		ModelID: frozen.ModelID, Profile: frozen.Profile, SnapshotIdentity: frozen.Identity(),
		PricingRevision: frozen.Pricing.Revision, PricingCurrency: frozen.Pricing.Currency, PricingUnit: frozen.Pricing.Unit,
		InputPrice: frozen.Pricing.Input, CachedInputPrice: frozen.Pricing.CachedInput, OutputPrice: frozen.Pricing.Output,
	}
	return WithModelInvocation(ctx, invocation)
}

func modelReservationIdentity(attempt *AttemptContext, ordinal uint64, catalogRef string) string {
	digest := sha256.New()
	for _, value := range []string{
		modelReservationDomain, attempt.Run.ID, strconv.FormatUint(uint64(attempt.Run.Attempt), 10),
		attempt.Trace.ID, strconv.FormatUint(ordinal, 10), catalogRef,
	} {
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
