package mysql

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// SeedAdmin 若用户表为空，创建默认 admin 用户。密码从 auth.seed.admin_password 读取，缺省为 admin123
func SeedAdmin(ctx context.Context) {
	db, err := DB(ctx)
	if err != nil {
		return
	}
	var count int64
	if db.Model(&User{}).Count(&count).Error != nil || count > 0 {
		return
	}
	pass, _ := g.Cfg().Get(ctx, "auth.seed.admin_password")
	password := "admin123"
	if pass != nil && pass.String() != "" {
		password = pass.String()
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	db.Create(&User{
		ID:       uuid.New().String(),
		Username: "admin",
		Password: string(hash),
		Role:     "admin",
	})
}

// SeedSettings 初始化默认设置项，仅在 key 不存在（值为空）时写入，不覆盖用户已修改的值。
func SeedSettings(ctx context.Context) {
	defaults := []struct{ key, value string }{
		{"general.site_name", "安全事件智能研判多智能体协同平台"},
		{"general.auto_mark_read", "true"},
	}
	for _, d := range defaults {
		if existing, _ := GetSetting(ctx, d.key); existing == "" {
			_ = SetSetting(ctx, d.key, d.value)
		}
	}
	// 自动生成 ingest API Key（首次启动时生成，后续不覆盖）
	if existing, _ := GetSetting(ctx, "ingest.api_key"); existing == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err == nil {
			_ = SetSetting(ctx, "ingest.api_key", "sk-ingest-"+hex.EncodeToString(b))
		}
	}
}

// SeedTermMappings 幂等写入安全域术语归一化种子数据（表为空才写入）。
func SeedTermMappings(ctx context.Context) {
	db, err := DB(ctx)
	if err != nil {
		return
	}
	var count int64
	if db.Model(&QueryTermMapping{}).Count(&count).Error != nil || count > 0 {
		return
	}

	seeds := []QueryTermMapping{
		// ── 漏洞利用缩写（priority 100）──────────────────────────────────────
		{SourceTerm: "rce", TargetTerm: "远程代码执行(RCE)", Priority: 100, Enabled: true},
		{SourceTerm: "ssrf", TargetTerm: "服务端请求伪造(SSRF)", Priority: 100, Enabled: true},
		{SourceTerm: "xss", TargetTerm: "跨站脚本攻击(XSS)", Priority: 100, Enabled: true},
		{SourceTerm: "sqli", TargetTerm: "SQL注入漏洞", Priority: 100, Enabled: true},
		{SourceTerm: "sql注入", TargetTerm: "SQL注入漏洞", Priority: 100, Enabled: true},
		{SourceTerm: "xxe", TargetTerm: "XML外部实体注入(XXE)", Priority: 100, Enabled: true},
		{SourceTerm: "csrf", TargetTerm: "跨站请求伪造(CSRF)", Priority: 100, Enabled: true},
		{SourceTerm: "lfi", TargetTerm: "本地文件包含(LFI)", Priority: 100, Enabled: true},
		{SourceTerm: "rfi", TargetTerm: "远程文件包含(RFI)", Priority: 100, Enabled: true},
		{SourceTerm: "idor", TargetTerm: "不安全直接对象引用(IDOR)越权", Priority: 100, Enabled: true},
		{SourceTerm: "ssti", TargetTerm: "服务端模板注入(SSTI)", Priority: 100, Enabled: true},
		{SourceTerm: "crlf", TargetTerm: "CRLF注入漏洞", Priority: 100, Enabled: true},
		{SourceTerm: "路径穿越", TargetTerm: "路径遍历漏洞(Path Traversal)", Priority: 100, Enabled: true},
		{SourceTerm: "目录遍历", TargetTerm: "路径遍历漏洞(Path Traversal)", Priority: 100, Enabled: true},
		{SourceTerm: "命令注入", TargetTerm: "OS命令注入漏洞", Priority: 100, Enabled: true},
		{SourceTerm: "代码注入", TargetTerm: "代码注入漏洞(Code Injection)", Priority: 100, Enabled: true},
		{SourceTerm: "内存溢出", TargetTerm: "缓冲区溢出/内存安全漏洞", Priority: 100, Enabled: true},
		{SourceTerm: "原型链污染", TargetTerm: "JavaScript原型链污染漏洞", Priority: 100, Enabled: true},

		// ── 认证/授权漏洞（priority 95）──────────────────────────────────────
		{SourceTerm: "jwt漏洞", TargetTerm: "JSON Web Token安全漏洞", Priority: 95, Enabled: true},
		{SourceTerm: "oauth漏洞", TargetTerm: "OAuth授权协议安全漏洞", Priority: 95, Enabled: true},
		{SourceTerm: "未授权访问", TargetTerm: "未授权访问漏洞(Unauthorized Access)", Priority: 95, Enabled: true},
		{SourceTerm: "弱口令", TargetTerm: "弱密码/默认凭证漏洞", Priority: 95, Enabled: true},
		{SourceTerm: "硬编码凭证", TargetTerm: "硬编码密码/凭证泄露漏洞", Priority: 95, Enabled: true},
		{SourceTerm: "会话劫持", TargetTerm: "会话劫持漏洞(Session Hijacking)", Priority: 95, Enabled: true},
		{SourceTerm: "权限绕过", TargetTerm: "访问控制绕过/越权漏洞", Priority: 95, Enabled: true},
		{SourceTerm: "aaa", TargetTerm: "认证授权审计(AAA)安全", Priority: 95, Enabled: true},

		// ── 容器/云原生（priority 90）─────────────────────────────────────────
		{SourceTerm: "k8s", TargetTerm: "Kubernetes", Priority: 90, Enabled: true},
		{SourceTerm: "k3s", TargetTerm: "K3s轻量级Kubernetes", Priority: 90, Enabled: true},
		{SourceTerm: "oci", TargetTerm: "OCI容器镜像规范", Priority: 90, Enabled: true},
		{SourceTerm: "eks", TargetTerm: "Amazon EKS托管Kubernetes", Priority: 90, Enabled: true},
		{SourceTerm: "gke", TargetTerm: "Google GKE托管Kubernetes", Priority: 90, Enabled: true},
		{SourceTerm: "aks", TargetTerm: "Azure AKS托管Kubernetes", Priority: 90, Enabled: true},
		{SourceTerm: "helm", TargetTerm: "Helm Kubernetes包管理器", Priority: 90, Enabled: true},
		{SourceTerm: "istio", TargetTerm: "Istio服务网格", Priority: 90, Enabled: true},
		{SourceTerm: "dockerfile", TargetTerm: "Docker容器镜像构建文件", Priority: 90, Enabled: true},
		{SourceTerm: "容器逃逸", TargetTerm: "容器逃逸漏洞(Container Escape)", Priority: 90, Enabled: true},
		{SourceTerm: "k8s rbac", TargetTerm: "Kubernetes基于角色的访问控制(RBAC)", Priority: 90, Enabled: true},
		{SourceTerm: "k8s漏洞", TargetTerm: "Kubernetes安全漏洞", Priority: 90, Enabled: true},

		// ── 组件/中间件别名（priority 80）────────────────────────────────────
		{SourceTerm: "log4shell", TargetTerm: "Apache Log4j 2远程代码执行漏洞(CVE-2021-44228)", Priority: 80, Enabled: true},
		{SourceTerm: "log4j2", TargetTerm: "Apache Log4j 2", Priority: 80, Enabled: true},
		{SourceTerm: "spring4shell", TargetTerm: "Spring Framework远程代码执行漏洞(CVE-2022-22965)", Priority: 80, Enabled: true},
		{SourceTerm: "springboot", TargetTerm: "Spring Boot", Priority: 80, Enabled: true},
		{SourceTerm: "spring boot", TargetTerm: "Spring Boot", Priority: 80, Enabled: true},
		{SourceTerm: "struts2", TargetTerm: "Apache Struts 2", Priority: 80, Enabled: true},
		{SourceTerm: "fastjson", TargetTerm: "FastJSON反序列化漏洞", Priority: 80, Enabled: true},
		{SourceTerm: "shiro漏洞", TargetTerm: "Apache Shiro反序列化/身份认证绕过漏洞", Priority: 80, Enabled: true},
		{SourceTerm: "shiro", TargetTerm: "Apache Shiro", Priority: 80, Enabled: true},
		{SourceTerm: "jackson", TargetTerm: "Jackson反序列化漏洞", Priority: 80, Enabled: true},
		{SourceTerm: "weblogic", TargetTerm: "Oracle WebLogic Server", Priority: 80, Enabled: true},
		{SourceTerm: "jboss", TargetTerm: "JBoss应用服务器", Priority: 80, Enabled: true},
		{SourceTerm: "tomcat", TargetTerm: "Apache Tomcat", Priority: 80, Enabled: true},
		{SourceTerm: "nginx", TargetTerm: "Nginx Web服务器", Priority: 80, Enabled: true},
		{SourceTerm: "openssl", TargetTerm: "OpenSSL", Priority: 80, Enabled: true},
		{SourceTerm: "heartbleed", TargetTerm: "OpenSSL心脏滴血漏洞(CVE-2014-0160)", Priority: 80, Enabled: true},
		{SourceTerm: "shellshock", TargetTerm: "Bash破壳漏洞(CVE-2014-6271)", Priority: 80, Enabled: true},
		{SourceTerm: "eternalblue", TargetTerm: "永恒之蓝SMB漏洞(CVE-2017-0144)", Priority: 80, Enabled: true},

		// ── 安全概念中英文（priority 70）─────────────────────────────────────
		{SourceTerm: "注入漏洞", TargetTerm: "注入类安全漏洞(Injection Vulnerability)", Priority: 70, Enabled: true},
		{SourceTerm: "越权", TargetTerm: "越权访问漏洞(Privilege Escalation)", Priority: 70, Enabled: true},
		{SourceTerm: "提权", TargetTerm: "权限提升漏洞(Privilege Escalation)", Priority: 70, Enabled: true},
		{SourceTerm: "命令执行", TargetTerm: "远程命令执行漏洞", Priority: 70, Enabled: true},
		{SourceTerm: "反序列化", TargetTerm: "反序列化漏洞(Deserialization Vulnerability)", Priority: 70, Enabled: true},
		{SourceTerm: "信息泄露", TargetTerm: "敏感信息泄露漏洞(Information Disclosure)", Priority: 70, Enabled: true},
		{SourceTerm: "拒绝服务", TargetTerm: "拒绝服务攻击(DoS/DDoS)", Priority: 70, Enabled: true},
		{SourceTerm: "dos", TargetTerm: "拒绝服务攻击(DoS)", Priority: 70, Enabled: true},
		{SourceTerm: "ddos", TargetTerm: "分布式拒绝服务攻击(DDoS)", Priority: 70, Enabled: true},
		{SourceTerm: "社工", TargetTerm: "社会工程学攻击(Social Engineering)", Priority: 70, Enabled: true},
		{SourceTerm: "钓鱼", TargetTerm: "网络钓鱼攻击(Phishing)", Priority: 70, Enabled: true},
		{SourceTerm: "apt", TargetTerm: "高级持续性威胁(APT)", Priority: 70, Enabled: true},
		{SourceTerm: "勒索", TargetTerm: "勒索软件攻击(Ransomware)", Priority: 70, Enabled: true},
		{SourceTerm: "挖矿", TargetTerm: "恶意挖矿软件(Cryptominer)", Priority: 70, Enabled: true},
		{SourceTerm: "后门", TargetTerm: "后门程序(Backdoor)", Priority: 70, Enabled: true},

		// ── 网络/协议攻击（priority 65）──────────────────────────────────────
		{SourceTerm: "mitm", TargetTerm: "中间人攻击(MITM)", Priority: 65, Enabled: true},
		{SourceTerm: "中间人", TargetTerm: "中间人攻击(MITM/Man-in-the-Middle)", Priority: 65, Enabled: true},
		{SourceTerm: "dns劫持", TargetTerm: "DNS劫持/DNS污染攻击", Priority: 65, Enabled: true},
		{SourceTerm: "arp欺骗", TargetTerm: "ARP欺骗/ARP投毒攻击", Priority: 65, Enabled: true},
		{SourceTerm: "端口扫描", TargetTerm: "端口扫描/网络侦察", Priority: 65, Enabled: true},
		{SourceTerm: "横向移动", TargetTerm: "横向移动(Lateral Movement)攻击技术", Priority: 65, Enabled: true},
		{SourceTerm: "c2", TargetTerm: "命令与控制(C2/C&C)服务器", Priority: 65, Enabled: true},
		{SourceTerm: "c&c", TargetTerm: "命令与控制(C2/C&C)服务器", Priority: 65, Enabled: true},

		// ── 时间规范化（priority 60）──────────────────────────────────────────
		{SourceTerm: "近期", TargetTerm: "最近7天", Priority: 60, Enabled: true},
		{SourceTerm: "最近", TargetTerm: "最近7天", Priority: 60, Enabled: true},
		{SourceTerm: "最新", TargetTerm: "最新发布的", Priority: 60, Enabled: true},
		{SourceTerm: "近来", TargetTerm: "最近7天内", Priority: 60, Enabled: true},
		{SourceTerm: "近几天", TargetTerm: "最近3天", Priority: 60, Enabled: true},
		{SourceTerm: "今天", TargetTerm: "今日(最近24小时)", Priority: 60, Enabled: true},
		{SourceTerm: "本周", TargetTerm: "本周(最近7天)", Priority: 60, Enabled: true},

		// ── 恶意软件类型（priority 55）────────────────────────────────────────
		{SourceTerm: "rat", TargetTerm: "远程访问木马(RAT)", Priority: 55, Enabled: true},
		{SourceTerm: "rootkit", TargetTerm: "Rootkit隐蔽恶意软件", Priority: 55, Enabled: true},
		{SourceTerm: "webshell", TargetTerm: "WebShell网页木马", Priority: 55, Enabled: true},
		{SourceTerm: "木马", TargetTerm: "木马病毒(Trojan)", Priority: 55, Enabled: true},
		{SourceTerm: "蠕虫", TargetTerm: "蠕虫病毒(Worm)", Priority: 55, Enabled: true},
		{SourceTerm: "僵尸网络", TargetTerm: "僵尸网络(Botnet)", Priority: 55, Enabled: true},

		// ── 安全评级/标准（priority 50）──────────────────────────────────────
		{SourceTerm: "0day", TargetTerm: "零日漏洞(0-day Vulnerability)", Priority: 50, Enabled: true},
		{SourceTerm: "0-day", TargetTerm: "零日漏洞(0-day Vulnerability)", Priority: 50, Enabled: true},
		{SourceTerm: "poc", TargetTerm: "漏洞概念验证代码(PoC)", Priority: 50, Enabled: true},
		{SourceTerm: "exp", TargetTerm: "漏洞利用代码(Exploit)", Priority: 50, Enabled: true},
		{SourceTerm: "exploit", TargetTerm: "漏洞利用代码(Exploit)", Priority: 50, Enabled: true},
		{SourceTerm: "cvss", TargetTerm: "通用漏洞评分系统(CVSS)", Priority: 50, Enabled: true},
		{SourceTerm: "cvssv3", TargetTerm: "通用漏洞评分系统v3(CVSSv3)", Priority: 50, Enabled: true},
		{SourceTerm: "nvd", TargetTerm: "国家漏洞数据库(NVD)", Priority: 50, Enabled: true},
	}

	_ = db.Create(&seeds).Error
}
