package intelligence

import (
	"context"
	"testing"

	dao "SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

func TestTransactionalEffectIntelligenceRecognizesBoundTransaction(t *testing.T) {
	ctx, err := dao.ContextWithTransaction(context.Background(), &gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	if !dao.HasBoundTransaction(ctx) {
		t.Fatal("transactional save_intelligence would start legacy asynchronous indexing")
	}
}
