package retrieval_test

// T2「Hybrid RAG 小规模真实评测」正式实验 harness。
//
// 真实部分：生产 Milvus 集合（19530 共享集合）+ 生产 documents 分区、
// 生产 dense / sparse / hybrid searcher（hybrid = Milvus 服务端 RRFReranker）、
// 生产 Embedding Provider（qwen3.7-text-embedding 2048 维）、生产应用层 ACL
// 过滤（retrieval.FilterDocuments）、生产 Query Rewrite（需 Chat Provider）。
//
// 评测路径说明（写进 README 与结论）：
//   * dense  / hybrid : 生产 Retriever（分模式配置构造，见下）
//   * BM25-only       : evaluation-only ablation，Harness 直接调用生产 SparseSearcher，
//                       不是业务暴露的 Retriever mode
//
// 本文件不修改任何产品代码；所有 fixture 文档与 payload 均为测试专用假数据。

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/document"
	"SentinelOps/internal/ai/embedder"
	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/retrieval"
	"SentinelOps/internal/ai/retrieval/searcher"
	"SentinelOps/internal/ai/rewrite"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/dao/milvus"
	knowledge "SentinelOps/internal/service/knowledge"
	pipeline "SentinelOps/internal/ai/agent/knowledge_index_pipeline"
	clientutil "SentinelOps/utility/client"

	"github.com/cloudwego/eino/components/embedding"
	driver "github.com/go-sql-driver/mysql"
	"github.com/cloudwego/eino/schema"
	milvuscli "github.com/milvus-io/milvus-sdk-go/v2/client"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	t2EvidenceDirEnv = "SENTINELOPS_T2_EVIDENCE_DIR"
	t2QuickEnv       = "SENTINELOPS_T2_QUICK"
	t2EvalUserID     = "t2-eval-user"
)

type t2FixtureDoc struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	AccessScope string `json:"access_scope"`
	Content     string `json:"content"`
}

type t2Query struct {
	ID          string   `json:"query_id"`
	Query       string   `json:"query"`
	Category    string   `json:"category"`
	Expected    []string `json:"expected_doc_keys"`
	Reason      string   `json:"reason"`
	ComplexOnly bool     `json:"complex_only,omitempty"`
}

type t2RunResult struct {
	QueryID     string   `json:"query_id"`
	Category    string   `json:"category"`
	Mode        string   `json:"mode"`
	QueryText   string   `json:"query_text"`
	RankedDocIDs []string `json:"ranked_doc_ids"`
	RankedScores []float64 `json:"ranked_scores,omitempty"`
	ExpectedDocIDs []string `json:"expected_doc_ids"`
	HitAt1      bool     `json:"hit_at_1"`
	HitAt3      bool     `json:"hit_at_3"`
	HitAt5      bool     `json:"hit_at_5"`
	ReciprocalRank float64 `json:"reciprocal_rank"`
	FirstHitRank int     `json:"first_hit_rank"`
	Degraded    bool     `json:"degraded,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type t2Metrics struct {
	Mode        string             `json:"mode"`
	Scope       string             `json:"scope"`
	Queries     int                `json:"queries"`
	HitAt1      float64            `json:"hit_at_1"`
	HitAt3      float64            `json:"hit_at_3"`
	HitAt5      float64            `json:"hit_at_5"`
	MRR         float64            `json:"mrr_at_5"`
	ByCategory  map[string]float64 `json:"by_category_hit_at_3"`
}

type t2ACLFacts struct {
	Query            string   `json:"query"`
	UnauthorizedDoc  string   `json:"unauthorized_doc_id"`
	AuthorizedDoc    string   `json:"authorized_doc_id"`
	Mode             string   `json:"mode"`
	RawCandidateIDs  []string `json:"raw_candidate_doc_ids"`
	RawContainsUnauthorized bool `json:"raw_candidates_contain_unauthorized"`
	FinalEvidenceIDs []string `json:"final_evidence_doc_ids"`
	FinalContainsUnauthorized bool `json:"final_evidence_contains_unauthorized"`
	FinalContainsAuthorized   bool `json:"final_evidence_contains_authorized"`
	Error            string   `json:"error,omitempty"`
}

// TestHybridRAGEvaluationEvidence 是 T2 主实验。
func TestHybridRAGEvaluationEvidence(t *testing.T) {
	evidenceRoot := strings.TrimSpace(os.Getenv(t2EvidenceDirEnv))
	if evidenceRoot == "" {
		t.Skip("T2 evidence harness: set " + t2EvidenceDirEnv + " to run the RAG evaluation")
	}
	evidenceDir, err := filepath.Abs(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t2RequireModelKey(t)
	repoRoot := t2RepoRoot(t)
	// 生产知识库服务要求显式身份（policy.Authorize）；评测 harness 使用本地
	// 运维身份写入一次性评测库，与请求侧 Scope 保持同一用户。
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: t2EvalUserID, Username: t2EvalUserID, Role: policy.RoleOperator,
		Scope: policy.Scope{UserID: t2EvalUserID},
	})
	if _, _, err := appconfig.LoadDirectory(filepath.Join(repoRoot, "manifest", "config")); err != nil {
		t.Fatalf("load application config: %v", err)
	}
	db, dsn := t2NewDisposableDatabase(t, "rag_eval")
	if err := dao.InitWithDSN(ctx, []byte(dsn)); err != nil {
		t.Fatalf("initialize application database: %v", err)
	}
	denseEmbedder, err := embedder.NewDenseEmbedder(ctx)
	if err != nil {
		t.Fatalf("initialize production dense embedder: %v", err)
	}
	milvusClient, err := milvus.GetClient(ctx)
	if err != nil {
		t.Fatalf("initialize production Milvus client: %v", err)
	}
	redisClient, err := clientutil.GetRedisClient(ctx)
	if err != nil {
		t.Fatalf("initialize production Redis client: %v", err)
	}

	quick := strings.TrimSpace(os.Getenv(t2QuickEnv)) == "1"
	tag := strconv.FormatInt(time.Now().Unix()%100000, 10)
	if configured := strings.TrimSpace(os.Getenv("SENTINELOPS_T2_TAG")); configured != "" {
		tag = configured
	}
	baseID := "t2eval-base-" + tag
	base, err := knowledge.CreateBase(ctx, "T2 RAG evaluation "+tag, "disposable evaluation knowledge base")
	if err != nil {
		t.Fatalf("create evaluation knowledge base: %v", err)
	}
	baseID = base.ID

	docs := t2FixtureDocs()
	queries := t2Queries()
	if quick {
		docs = docs[:4]
		queries = queries[:4]
	}
	docIDs := map[string]string{}
	for _, fixture := range docs {
		docID := fmt.Sprintf("t2eval-%s-%s", tag, fixture.Key)
		if err := t2IndexFixtureDoc(ctx, db, baseID, docID, fixture); err != nil {
			t.Fatalf("index fixture doc %s: %v", fixture.Key, err)
		}
		docIDs[fixture.Key] = docID
	}
	t.Cleanup(func() {
		for _, docID := range docIDs {
			cleanupCtx := policy.WithIdentity(context.Background(), policy.Identity{
				UserID: t2EvalUserID, Username: t2EvalUserID, Role: policy.RoleOperator,
				Scope: policy.Scope{UserID: t2EvalUserID},
			})
			if err := knowledge.DeleteDoc(cleanupCtx, docID); err != nil {
				t.Logf("cleanup eval doc %s: %v", docID, err)
			}
		}
	})

	partition := milvus.PartitionDocuments
	baseCfg := retrieval.Config{
		EmbeddingModel: "qwen3.7-text-embedding", CacheTTL: 24 * time.Hour, CacheThreshold: 0.85,
		TopK: 5, FinalTopK: 5, MinScore: 0.30, RRFK: 60, Partition: partition,
	}
	scope := evidence.Scope{UserID: t2EvalUserID, Role: "operator", AccessScope: "user:" + t2EvalUserID}
	scopeCtx := evidence.WithScope(ctx, scope)

	denseCfg := baseCfg
	denseCfg.HybridEnabled = false
	denseCfg.CacheKeyPrefix = "rag:eval:dense:" + tag
	hybridCfg := baseCfg
	hybridCfg.HybridEnabled = true
	hybridCfg.CacheKeyPrefix = "rag:eval:hybrid:" + tag
	denseRetriever := retrieval.New(milvusClient, denseEmbedder, redisClient, denseCfg)
	hybridRetriever := retrieval.New(milvusClient, denseEmbedder, redisClient, hybridCfg)

	results := make([]t2RunResult, 0, len(queries)*4)
	for _, item := range queries {
		expected := t2ExpectedDocIDs(item, docIDs)
		run := func(mode, queryText string, retrieve func(string) ([]*schema.Document, error)) {
			result := t2RunResult{
				QueryID: item.ID, Category: item.Category, Mode: mode, QueryText: queryText,
				ExpectedDocIDs: expected, FirstHitRank: 0,
			}
			started := time.Now()
			documents, err := retrieve(queryText)
			_ = started
			if err != nil {
				result.Error = err.Error()
				results = append(results, result)
				return
			}
			result.RankedDocIDs, result.RankedScores = t2RankDocuments(documents)
			t2ScoreResult(&result)
			results = append(results, result)
		}
		run("dense", item.Query, func(query string) ([]*schema.Document, error) {
			return denseRetriever.Retrieve(scopeCtx, query)
		})
		run("bm25", item.Query, func(query string) ([]*schema.Document, error) {
			raw, err := searcher.NewSparseSearcher(milvusClient, baseCfg.FinalTopK, partition).Search(scopeCtx, query)
			if err != nil {
				return nil, err
			}
			return retrieval.FilterDocuments(scopeCtx, raw, scope)
		})
		run("hybrid", item.Query, func(query string) ([]*schema.Document, error) {
			return hybridRetriever.Retrieve(scopeCtx, query)
		})
		if item.Category == "complex" {
			rewritten := rewrite.RewriteQuery(ctx, item.Query, nil)
			if rewritten != "" && rewritten != item.Query {
				run("hybrid+rewrite", rewritten, func(query string) ([]*schema.Document, error) {
					return hybridRetriever.Retrieve(scopeCtx, query)
				})
			}
		}
	}

	metrics := t2ComputeMetrics(results)
	aclFacts := t2RunACLCase(t, scopeCtx, milvusClient, denseEmbedder, baseCfg, docIDs)
	t2WriteJSON(t, filepath.Join(evidenceDir, "dataset.json"), map[string]any{
		"knowledge_base_id": baseID, "documents": docs, "queries": queries,
		"document_ids": docIDs, "scope": scope, "note": "fixture 由 harness 通过生产 BuildAndIndex + 同契约 MySQL 投影构造",
	})
	t2WriteJSON(t, filepath.Join(evidenceDir, "dense-results.json"), t2FilterMode(results, "dense"))
	t2WriteJSON(t, filepath.Join(evidenceDir, "bm25-results.json"), t2FilterMode(results, "bm25"))
	t2WriteJSON(t, filepath.Join(evidenceDir, "hybrid-results.json"), t2FilterMode(results, "hybrid"))
	t2WriteJSON(t, filepath.Join(evidenceDir, "rewrite-results.json"), t2FilterMode(results, "hybrid+rewrite"))
	t2WriteJSON(t, filepath.Join(evidenceDir, "acl-results.json"), aclFacts)
	t2WriteJSON(t, filepath.Join(evidenceDir, "metrics.json"), map[string]any{
		"metrics": metrics,
		"config": map[string]any{
			"dense": map[string]any{"hybrid_enabled": false, "top_k": baseCfg.TopK, "final_top_k": baseCfg.FinalTopK, "min_score": baseCfg.MinScore},
			"hybrid": map[string]any{"hybrid_enabled": true, "rrf": "Milvus NewRRFReranker()", "configured_rrf_k": baseCfg.RRFK,
				"note": "Go RRFK 配置只进入 Trace Span，不改变服务端融合参数；服务端使用标准 RRF 默认 k=60"},
			"bm25": map[string]any{"path": "evaluation-only ablation: searcher.SparseSearcher 直调", "idf": false, "tokenizer": "空白/标点切分 + 小写"},
		},
		"sample_size": len(queries),
	})
	t2WriteCSV(t, filepath.Join(evidenceDir, "results.csv"), results)

	for _, metric := range metrics {
		t.Logf("T2 %s: hit@1=%.3f hit@3=%.3f hit@5=%.3f mrr=%.3f by_category=%v",
			metric.Scope+" "+metric.Mode, metric.HitAt1, metric.HitAt3, metric.HitAt5, metric.MRR, metric.ByCategory)
	}
	for _, fact := range aclFacts {
		t.Logf("T2 acl %s/%s: raw_contains_unauthorized=%v final_contains_unauthorized=%v final_contains_authorized=%v",
			fact.Query, fact.Mode, fact.RawContainsUnauthorized, fact.FinalContainsUnauthorized, fact.FinalContainsAuthorized)
	}
}

func t2RequireModelKey(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("SENTINELOPS_MODEL_API_KEY")) == "" {
		t.Skip("T2 requires SENTINELOPS_MODEL_API_KEY (SET/MISSING only, value never printed): SKIPPED_BLOCKED_BY_SECRET")
	}
}

func t2RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifest", "config", "config.local.yaml")); err != nil {
		t.Fatalf("config.local.yaml not found from %s: %v", root, err)
	}
	return root
}

func t2NewDisposableDatabase(t *testing.T, suffix string) (*gorm.DB, string) {
	t.Helper()
	baseDSN := os.Getenv("SENTINELOPS_TEST_DSN")
	if baseDSN == "" {
		t.Fatal("SENTINELOPS_TEST_DSN is required (disposable sentinelops_phase03 database)")
	}
	config, err := driver.ParseDSN(baseDSN)
	if err != nil {
		t.Fatalf("parse base test DSN: %v", err)
	}
	if config.DBName != "sentinelops_phase03" {
		t.Fatalf("refuse non-disposable database %q", config.DBName)
	}
	digest := sha256.Sum256([]byte(suffix))
	databaseName := fmt.Sprintf("sentinelops_phase03_t2_%x", digest[:6])
	adminConfig := *config
	adminConfig.DBName = "mysql"
	adminDB, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	quoted := "`" + databaseName + "`"
	if _, err := adminDB.Exec("DROP DATABASE IF EXISTS " + quoted); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.Exec("CREATE DATABASE " + quoted + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP DATABASE IF EXISTS " + quoted) })
	testConfig := *config
	testConfig.DBName = databaseName
	testDSN := testConfig.FormatDSN()
	gooseBinary := os.Getenv("SENTINELOPS_GOOSE_BIN")
	if gooseBinary == "" {
		gooseBinary, err = exec.LookPath("goose")
		if err != nil {
			t.Fatal("goose v3.27.3 is required for the T2 evaluation")
		}
	}
	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gooseBinary, "-dir", migrationDir, "mysql", testDSN, "up").CombinedOutput(); err != nil {
		t.Fatalf("goose up: %v\n%s", err, strings.ReplaceAll(string(output), testDSN, "<redacted-dsn>"))
	}
	sqlDB, err := sql.Open("mysql", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db, testDSN
}

func t2IndexFixtureDoc(ctx context.Context, db *gorm.DB, baseID, docID string, fixture t2FixtureDoc) error {
	dir, err := os.MkdirTemp("", "t2-fixture-*")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fixture.Key+".md")
	if err := os.WriteFile(path, []byte(fixture.Content), 0o600); err != nil {
		return err
	}
	contentHash := sha256.Sum256([]byte(fixture.Content))
	hashHex := hex.EncodeToString(contentHash[:])
	const indexedVersion = uint64(1)
	chunks, err := pipeline.BuildAndIndex(ctx, pipeline.IndexInput{
		FilePath: path, BaseID: baseID, DocID: docID, DocTitle: fixture.Title,
		SourceVersion: "t2-eval-v1", ContentHash: hashHex, AccessScope: fixture.AccessScope,
		IndexedVersion: indexedVersion,
		Config: document.ChunkConfig{Strategy: document.StrategyHierarchical, ParentChunkSize: 1024, ChildChunkSize: 256},
	})
	if err != nil {
		return err
	}
	now := time.Now()
	chunkConfigJSON, err := json.Marshal(document.ChunkConfig{Strategy: document.StrategyHierarchical, ParentChunkSize: 1024, ChildChunkSize: 256})
	if err != nil {
		return err
	}
	docRow := dao.KnowledgeDocument{
		ID: docID, BaseID: baseID, Name: fixture.Key + ".md", FilePath: path,
		FileSize: int64(len(fixture.Content)), FileType: "md", FileHash: hashHex,
		ChunkStrategy: string(document.StrategyHierarchical), ChunkConfig: string(chunkConfigJSON), ChunkCount: len(chunks),
		IndexedChunks: len(chunks), IndexedAt: &now, IndexStatus: "completed", Enabled: true,
		ContentHash: hashHex, SourceVersion: "t2-eval-v1", AccessScope: fixture.AccessScope,
		IndexedVersion: indexedVersion, UpdatedBy: "t2-eval-harness",
	}
	if err := db.Create(&docRow).Error; err != nil {
		return err
	}
	for _, chunk := range chunks {
		row := dao.KnowledgeChunk{
			ID: chunk.ID, DocID: docID, ChunkIndex: chunk.ChunkIndex, ContentPreview: chunk.Content,
			SectionTitle: chunk.SectionTitle, CharCount: chunk.CharCount, Enabled: true,
			ContentHash: hashHex, SourceVersion: "t2-eval-v1", AccessScope: fixture.AccessScope,
			IndexedVersion: indexedVersion, UpdatedBy: "t2-eval-harness",
		}
		if err := db.Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func t2RankDocuments(documents []*schema.Document) ([]string, []float64) {
	ids := make([]string, 0, len(documents))
	scores := make([]float64, 0, len(documents))
	seen := map[string]bool{}
	for _, doc := range documents {
		if doc == nil {
			continue
		}
		docID, _ := doc.MetaData["doc_id"].(string)
		if docID == "" || seen[docID] {
			continue
		}
		seen[docID] = true
		ids = append(ids, docID)
		if score, ok := doc.MetaData["score"]; ok {
			if value, ok := score.(float64); ok {
				scores = append(scores, value)
				continue
			}
		}
		scores = append(scores, doc.Score())
	}
	return ids, scores
}

func t2ScoreResult(result *t2RunResult) {
	expected := map[string]bool{}
	for _, docID := range result.ExpectedDocIDs {
		expected[docID] = true
	}
	result.HitAt1 = t2HitWithin(result.RankedDocIDs, expected, 1)
	result.HitAt3 = t2HitWithin(result.RankedDocIDs, expected, 3)
	result.HitAt5 = t2HitWithin(result.RankedDocIDs, expected, 5)
	for index, docID := range result.RankedDocIDs {
		if expected[docID] {
			result.FirstHitRank = index + 1
			if index < 5 {
				result.ReciprocalRank = 1 / float64(index+1)
			}
			return
		}
	}
}

func t2HitWithin(ranked []string, expected map[string]bool, limit int) bool {
	for index, docID := range ranked {
		if index >= limit {
			return false
		}
		if expected[docID] {
			return true
		}
	}
	return false
}

func t2ExpectedDocIDs(item t2Query, docIDs map[string]string) []string {
	ids := make([]string, 0, len(item.Expected))
	for _, key := range item.Expected {
		if docID, ok := docIDs[key]; ok {
			ids = append(ids, docID)
		}
	}
	return ids
}

// t2FixtureDocs 是确定性 Ground Truth 语料：每篇文档围绕一个唯一事实，
// keyword 型含精确实体（CVE / IP / 告警编号 / 错误码），semantic 型只描述语义，
// ACL 型包含一篇无权文档与一篇公开对照文档。
func t2FixtureDocs() []t2FixtureDoc {
	return []t2FixtureDoc{
		{
			Key: "k1", Title: "GlobalProtect 命令注入漏洞公告", AccessScope: "public",
			Content: "漏洞编号 CVE-2024-3400 是 Palo Alto Networks PAN-OS GlobalProtect 功能的命令注入漏洞，未认证攻击者可通过构造请求执行任意命令。修复版本为 PAN-OS 11.1.2-h3、11.0.4-h1 与 10.2.9-h1，建议立即升级并在升级前启用威胁防护签名。",
		},
		{
			Key: "k2", Title: "IOC: 外联 C2 地址", AccessScope: "public",
			Content: "威胁情报 IOC：恶意 C2 地址 203.0.113.77，对应域名 cdn-metrics.example.net，使用 TCP 8443 端口做心跳。该地址在多个客户环境被发现与 Cobalt Strike 上线行为相关，建议在出口防火墙与 DNS 日志中同时布控。",
		},
		{
			Key: "k3", Title: "Elasticsearch 版本升级建议", AccessScope: "public",
			Content: "搜索集群组件 Elasticsearch 8.6.1 存在越权读取索引的缺陷，官方已在 Elasticsearch 8.6.2 修复。升级前需要确认插件兼容性，并按滚动重启流程逐节点升级，避免集群状态变红。",
		},
		{
			Key: "k4", Title: "告警 ALT-2024-0912 详情", AccessScope: "public",
			Content: "告警编号 ALT-2024-0912 对应 Redis 未授权访问尝试，源 IP 198.51.100.23 在短时间内对 6379 端口执行了 CONFIG SET 与 SLAVEOF 命令。处置建议：限制 Redis 监听地址、启用 requirepass，并在边界封禁该源地址。",
		},
		{
			Key: "k5", Title: "YARA 规则说明", AccessScope: "public",
			Content: "YARA 规则 Sentinel_CobaltStrike_Beacon_v3 用于匹配 Cobalt Strike 默认 named pipe 命名特征，覆盖 msagent_ 与 MSSE- 前缀变体。该规则只做内存扫描，不产生网络请求，命中后应结合进程树与网络连接共同研判。",
		},
		{
			Key: "k6", Title: "etcd 存储空间告警处置", AccessScope: "public",
			Content: "Kubernetes 控制面报错 etcdserver: mvcc: database space exceeded，对应错误码 ETCD_MVCC_DB_SPACE。原因是 etcd 的历史版本未压缩，需要执行 compaction 与 defrag，并检查 apiserver 是否有异常高频写入。",
		},
		{
			Key: "s1", Title: "账号权限收敛实践", AccessScope: "public",
			Content: "为了降低横向移动风险，应对数据库账号做最小权限收敛：应用账号只保留 DML 权限、禁止 DDL，运维账号按环境分离，并定期回收离职人员权限。权限收敛后，即使凭证泄露，攻击者能做的操作也被限制在很小的范围内。",
		},
		{
			Key: "s2", Title: "告警降噪与聚合策略", AccessScope: "public",
			Content: "在告警聚合阶段引入时间窗口去重，把五分钟内来自同一源地址、同一规则的多条重复告警合并为一条事件，并附带命中次数。这样可以显著减少值班人员的重复处理负担，避免真正的高危事件被淹没在噪音里。",
		},
		{
			Key: "s3", Title: "DNS 隧道检测思路", AccessScope: "public",
			Content: "数据外传常借助域名解析通道完成：检测思路是统计异常长度的子域名、单位时间内高频的 TXT 与 NULL 查询，以及同一客户端解析大量随机字符串域名。把这些特征与主机出网行为关联，可以发现隐蔽的数据渗出。",
		},
		{
			Key: "s4", Title: "容器镜像准入控制", AccessScope: "public",
			Content: "集群准入控制应校验镜像签名：只允许由可信签名机构签发的镜像被调度，同时禁止使用 latest 标签与未登记的私有仓库镜像。这样任何未经过构建流水线审核的镜像都无法在生产集群中启动。",
		},
		{
			Key: "s5", Title: "勒索软件防护与备份策略", AccessScope: "public",
			Content: "应对勒索软件时，备份必须离线或不可变存储保存，并且定期做恢复演练验证可用性；否则攻击者在加密主数据后，会继续加密可写备份，让恢复能力一并失效。恢复演练还能暴露备份策略中的单点依赖。",
		},
		{
			Key: "h1", Title: "Log4j2 JNDI 注入处置", AccessScope: "public",
			Content: "Log4j2 的 JNDI 注入漏洞 CVE-2021-44228 通过日志内容中的 ${jndi:ldap://...} 触发远程类加载。处置方式是升级到 2.17.1 以上版本，同时在 WAF 与日志采集侧检测 jndi:ldap 模式，临时缓解可设置 log4j2.formatMsgNoLookups=true。",
		},
		{
			Key: "h2", Title: "Nginx 上传 413 排障", AccessScope: "public",
			Content: "Nginx 返回 413 Request Entity Too Large 通常是 client_max_body_size 配置过小导致，默认值是 1m。结合业务上传需求可将该值调整为 200m，同时确认后端应用层与网关层的大小限制保持一致。",
		},
		{
			Key: "h3", Title: "EternalBlue 漏洞防护", AccessScope: "public",
			Content: "MS17-010（EternalBlue）利用 SMBv1 协议漏洞在 445 端口传播。防护手段是禁用 SMBv1、封禁 445 端口的跨网段访问、安装对应补丁，并对仍在使用老协议的文件共享服务做隔离。",
		},
		{
			Key: "h4", Title: "容器日志占满磁盘处置", AccessScope: "public",
			Content: "Docker 默认 json-file 日志驱动不会自动轮转，容器持续输出会让磁盘被写满。应在 daemon.json 配置 log-opts 的 max-size=50m 与 max-file=3，并在编排层为日志目录设置独立数据盘与容量告警。",
		},
		{
			Key: "acl_internal", Title: "内部凭据轮换流程（受限）", AccessScope: "user:t2-other-user",
			Content: "内部凭据轮换流程：财务系统数据库口令每 90 天通过 Vault 自动轮换一次，轮换窗口为周二凌晨 2 点；轮换后由值班工程师在内部工单系统确认应用连接正常。该流程文档仅限本团队成员查阅。",
		},
		{
			Key: "acl_public", Title: "凭据轮换通用建议", AccessScope: "public",
			Content: "凭据轮换的通用建议是设置周期性轮换策略、使用集中式密钥管理、轮换后做连通性验证。具体到不同系统，轮换周期与执行窗口应由各团队按自身变更流程确定。",
		},
	}
}

func t2Queries() []t2Query {
	return []t2Query{
		{ID: "kw-cve", Query: "CVE-2024-3400 的修复版本是什么", Category: "keyword", Expected: []string{"k1"}, Reason: "精确 CVE 编号"},
		{ID: "kw-ip", Query: "203.0.113.77 这个地址是什么", Category: "keyword", Expected: []string{"k2"}, Reason: "精确 IOC 地址"},
		{ID: "kw-elastic", Query: "Elasticsearch 8.6.1 漏洞", Category: "keyword", Expected: []string{"k3"}, Reason: "产品名 + 版本号"},
		{ID: "kw-alert", Query: "ALT-2024-0912", Category: "keyword", Expected: []string{"k4"}, Reason: "精确告警编号"},
		{ID: "kw-yara", Query: "Sentinel_CobaltStrike_Beacon_v3 YARA 规则", Category: "keyword", Expected: []string{"k5"}, Reason: "精确规则名"},
		{ID: "kw-etcd", Query: "ETCD_MVCC_DB_SPACE", Category: "keyword", Expected: []string{"k6"}, Reason: "精确错误码"},
		{ID: "kw-panos", Query: "PAN-OS 11.1.2-h3 修复了哪个漏洞", Category: "keyword", Expected: []string{"k1"}, Reason: "补丁版本精确实体"},
		{ID: "sem-least", Query: "怎么防止攻击者利用泄露的账号继续扩大战果", Category: "semantic", Expected: []string{"s1"}, Reason: "语义改写，无字面重合"},
		{ID: "sem-noise", Query: "值班同学被海量重复提醒淹没怎么办", Category: "semantic", Expected: []string{"s2"}, Reason: "语义改写"},
		{ID: "sem-dns", Query: "如何发现有人利用域名解析偷偷把数据传出去", Category: "semantic", Expected: []string{"s3"}, Reason: "语义改写"},
		{ID: "sem-image", Query: "怎么保证集群里跑起来的镜像都是经过审核的", Category: "semantic", Expected: []string{"s4"}, Reason: "语义改写"},
		{ID: "sem-backup", Query: "勒索病毒把备份也加密了，应该提前做什么准备", Category: "semantic", Expected: []string{"s5"}, Reason: "语义改写"},
		{ID: "sem-dedup", Query: "怎么减少相同来源的重复报警", Category: "semantic", Expected: []string{"s2"}, Reason: "语义改写（同文档第二问）"},
		{ID: "hyb-log4j", Query: "日志里出现 jndi ldap 字样是不是 Log4j 漏洞", Category: "hybrid", Expected: []string{"h1"}, Reason: "实体 + 语义混合"},
		{ID: "hyb-413", Query: "上传大文件返回 413 和 nginx 配置有关吗", Category: "hybrid", Expected: []string{"h2"}, Reason: "状态码 + 语义"},
		{ID: "hyb-eb", Query: "EternalBlue 怎么防护", Category: "hybrid", Expected: []string{"h3"}, Reason: "攻击名 + 语义"},
		{ID: "hyb-smb", Query: "SMBv1 还能用吗，有什么风险", Category: "hybrid", Expected: []string{"h3"}, Reason: "协议名 + 语义"},
		{ID: "cx-port445", Query: "之前那个 445 端口的老漏洞，现在应该怎么彻底关闭", Category: "complex", Expected: []string{"h3"}, Reason: "口语化、需要补全实体"},
		{ID: "cx-disk", Query: "容器日志把磁盘写满的问题，现在配置好了吗", Category: "complex", Expected: []string{"h4"}, Reason: "口语化、指代上文现象"},
		{ID: "cx-dbperm", Query: "上次说的数据库账号权限太宽的问题，具体怎么落地", Category: "complex", Expected: []string{"s1"}, Reason: "口语化 + 同义改写"},
		{ID: "cx-cve-recall", Query: "那个 CVE-2024-3400 的告警后来又出现了吗", Category: "complex", Expected: []string{"k1"}, Reason: "实体 + 会话式指代"},
		{ID: "cx-legacy-share", Query: "内网里那些还在用老协议的文件共享服务，除了打补丁还能怎么限制", Category: "complex", Expected: []string{"h3"}, Reason: "场景化描述，需补全 SMB 实体"},
		{ID: "acl-rotation", Query: "内部凭据轮换流程", Category: "acl", Expected: []string{"acl_public"}, Reason: "有权文档应保留，无权高度相关文档必须被过滤"},
	}
}

func t2FilterMode(results []t2RunResult, mode string) []t2RunResult {
	filtered := make([]t2RunResult, 0, len(results))
	for _, result := range results {
		if result.Mode == mode {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func t2ComputeMetrics(results []t2RunResult) []t2Metrics {
	byMode := map[string][]t2RunResult{}
	for _, result := range results {
		byMode[result.Mode] = append(byMode[result.Mode], result)
	}
	modes := make([]string, 0, len(byMode))
	for mode := range byMode {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	metrics := make([]t2Metrics, 0, len(modes)*4)
	for _, mode := range modes {
		list := byMode[mode]
		metrics = append(metrics, t2MetricFor("all", mode, list))
		categories := map[string][]t2RunResult{}
		for _, result := range list {
			categories[result.Category] = append(categories[result.Category], result)
		}
		names := make([]string, 0, len(categories))
		for name := range categories {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			metrics = append(metrics, t2MetricFor(name, mode, categories[name]))
		}
	}
	return metrics
}

func t2MetricFor(scope, mode string, results []t2RunResult) t2Metrics {
	metric := t2Metrics{Mode: mode, Scope: scope, Queries: len(results), ByCategory: map[string]float64{}}
	if len(results) == 0 {
		return metric
	}
	categoryTotals := map[string]int{}
	categoryHits := map[string]int{}
	for _, result := range results {
		if result.HitAt1 {
			metric.HitAt1++
		}
		if result.HitAt3 {
			metric.HitAt3++
		}
		if result.HitAt5 {
			metric.HitAt5++
		}
		metric.MRR += result.ReciprocalRank
		categoryTotals[result.Category]++
		if result.HitAt3 {
			categoryHits[result.Category]++
		}
	}
	count := float64(len(results))
	metric.HitAt1 /= count
	metric.HitAt3 /= count
	metric.HitAt5 /= count
	metric.MRR /= count
	for category, total := range categoryTotals {
		metric.ByCategory[category] = float64(categoryHits[category]) / float64(total)
	}
	return metric
}

// t2RunACLCase 验证“无权文档可能出现在 Milvus 召回候选，但不会进入最终 evidence”。
// 观察点在 harness 侧：同一 query 先取 searcher 原始返回（未过滤），再走生产
// FilterDocuments，两个集合分别记录。
func t2RunACLCase(
	t *testing.T,
	scopeCtx context.Context,
	milvusClient milvuscli.Client,
	denseEmbedder embedding.Embedder,
	cfg retrieval.Config,
	docIDs map[string]string,
) []t2ACLFacts {
	t.Helper()
	query := "内部凭据轮换流程"
	scope, _ := evidence.ScopeFromContext(scopeCtx)
	vectors, err := denseEmbedder.EmbedStrings(scopeCtx, []string{query})
	if err != nil {
		return []t2ACLFacts{{Query: query, Error: "embed acl query: " + err.Error()}}
	}
	if len(vectors) == 0 {
		return []t2ACLFacts{{Query: query, Error: "embed acl query returned empty vector"}}
	}
	facts := make([]t2ACLFacts, 0, 2)
	unauthorized := docIDs["acl_internal"]
	authorized := docIDs["acl_public"]
	modes := []struct {
		name  string
		raw   func() ([]*schema.Document, error)
	}{
		{"dense", func() ([]*schema.Document, error) {
			return searcher.NewDenseSearcher(milvusClient, cfg.TopK, cfg.Partition).Search(scopeCtx, vectors[0])
		}},
		{"hybrid", func() ([]*schema.Document, error) {
			return searcher.NewHybridSearcher(milvusClient, cfg.TopK, cfg.Partition, cfg.RRFK).Search(scopeCtx, query, vectors[0])
		}},
	}
	for _, mode := range modes {
		fact := t2ACLFacts{Query: query, Mode: mode.name, UnauthorizedDoc: unauthorized, AuthorizedDoc: authorized}
		raw, err := mode.raw()
		if err != nil {
			fact.Error = err.Error()
			facts = append(facts, fact)
			continue
		}
		fact.RawCandidateIDs, _ = t2RankDocuments(raw)
		for _, docID := range fact.RawCandidateIDs {
			if docID == unauthorized {
				fact.RawContainsUnauthorized = true
			}
		}
		filtered, err := retrieval.FilterDocuments(scopeCtx, raw, scope)
		if err != nil {
			fact.Error = err.Error()
			facts = append(facts, fact)
			continue
		}
		fact.FinalEvidenceIDs, _ = t2RankDocuments(filtered)
		for _, docID := range fact.FinalEvidenceIDs {
			if docID == unauthorized {
				fact.FinalContainsUnauthorized = true
			}
			if docID == authorized {
				fact.FinalContainsAuthorized = true
			}
		}
		facts = append(facts, fact)
	}
	return facts
}

func t2WriteCSV(t *testing.T, path string, results []t2RunResult) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"query_id", "category", "mode", "query", "rank_1_doc", "first_hit_rank", "hit_at_1", "hit_at_3", "hit_at_5", "reciprocal_rank", "ranked_doc_ids", "expected_doc_ids", "error"}); err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		top := ""
		if len(result.RankedDocIDs) > 0 {
			top = result.RankedDocIDs[0]
		}
		record := []string{
			result.QueryID, result.Category, result.Mode, result.QueryText, top,
			strconv.Itoa(result.FirstHitRank), strconv.FormatBool(result.HitAt1), strconv.FormatBool(result.HitAt3),
			strconv.FormatBool(result.HitAt5), fmt.Sprintf("%.4f", result.ReciprocalRank),
			strings.Join(result.RankedDocIDs, "|"), strings.Join(result.ExpectedDocIDs, "|"), result.Error,
		}
		if err := writer.Write(record); err != nil {
			t.Fatal(err)
		}
	}
}

func t2WriteJSON(t *testing.T, path string, payload any) {
	t.Helper()
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
