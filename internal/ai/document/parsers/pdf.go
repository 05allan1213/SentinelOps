package parsers

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dslipak/pdf"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// ParsePDF 提取 PDF 文件的纯文本内容。
// 解析流程（三阶段 fallback）：
//  1. 直接用 ledongthuc/pdf 打开并提取（快速路径，覆盖大多数标准 PDF）
//  2. 若失败，用 pdfcpu Optimize 修复格式后重试（处理损坏/非标准 PDF）
//  3. 若仍失败，用 pdfcpu Decrypt 去加密后重试（处理权限加密 PDF）
//  4. 三者均无文字时返回明确错误（如纯图片 PDF）
func ParsePDF(filePath string) (string, error) {
	// 阶段1：直接提取
	if text, err := parsePDFWithLib(filePath); err == nil {
		return text, nil
	} else {
		log.Printf("[parsePDF] stage1 failed (%s): %v", filepath.Base(filePath), err)
	}

	// 阶段2：pdfcpu Optimize 修复格式后重试
	if normalized, err := optimizePDFWithPdfcpu(filePath); err == nil {
		defer os.Remove(normalized)
		if text, err := parsePDFWithLib(normalized); err == nil {
			return text, nil
		} else {
			log.Printf("[parsePDF] stage2 failed (%s): %v", filepath.Base(filePath), err)
		}
	} else {
		log.Printf("[parsePDF] stage2 optimize failed (%s): %v", filepath.Base(filePath), err)
	}

	// 阶段3：pdfcpu Decrypt 去加密后重试
	decrypted, decErr := decryptPDFWithPdfcpu(filePath)
	if decErr != nil {
		log.Printf("[parsePDF] stage3 decrypt failed (%s): %v", filepath.Base(filePath), decErr)
	} else {
		defer os.Remove(decrypted)
		if text, err := parsePDFWithLib(decrypted); err == nil {
			return text, nil
		} else {
			log.Printf("[parsePDF] stage3 failed (%s): %v", filepath.Base(filePath), err)
		}

		// 阶段4：Decrypt 后强制转为传统 xref table 格式
		if reopt, err := normalizePDFToLegacy(decrypted); err == nil {
			defer os.Remove(reopt)
			if text, err := parsePDFWithLib(reopt); err == nil {
				return text, nil
			} else {
				log.Printf("[parsePDF] stage4 failed (%s): %v", filepath.Base(filePath), err)
			}
		} else {
			log.Printf("[parsePDF] stage4 normalize failed (%s): %v", filepath.Base(filePath), err)
		}
	}

	return "", fmt.Errorf("parse pdf %s: unable to extract text (possibly image-only or severely malformed PDF)", filepath.Base(filePath))
}

// optimizePDFWithPdfcpu 使用 pdfcpu 优化修复 PDF 格式问题。
func optimizePDFWithPdfcpu(src string) (string, error) {
	tmp, err := os.CreateTemp("", "fo-pdf-opt-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmp.Close()

	conf := model.NewDefaultConfiguration()
	conf.UserPW = ""
	conf.OwnerPW = ""
	if err := pdfcpuapi.OptimizeFile(src, tmp.Name(), conf); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("pdfcpu optimize: %w", err)
	}
	return tmp.Name(), nil
}

// normalizePDFToLegacy 将 PDF 转换为传统 xref table 格式（禁用 XRefStream/ObjectStream）。
func normalizePDFToLegacy(src string) (string, error) {
	tmp, err := os.CreateTemp("", "fo-pdf-legacy-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmp.Close()

	conf := model.NewDefaultConfiguration()
	conf.UserPW = ""
	conf.OwnerPW = ""
	conf.WriteXRefStream = false
	conf.WriteObjectStream = false
	if err := pdfcpuapi.OptimizeFile(src, tmp.Name(), conf); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("pdfcpu normalize legacy: %w", err)
	}
	return tmp.Name(), nil
}

// decryptPDFWithPdfcpu 移除 PDF 加密保护，返回解密后的临时文件路径。
func decryptPDFWithPdfcpu(src string) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("open src: %w", err)
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	conf.UserPW = ""
	conf.OwnerPW = ""
	conf.WriteXRefStream = false
	conf.WriteObjectStream = false

	ctx, err := pdfcpuapi.ReadAndValidate(f, conf)
	if err != nil {
		return "", fmt.Errorf("pdfcpu read: %w", err)
	}

	ctx.Encrypt = nil
	ctx.EncKey = nil

	tmp, err := os.CreateTemp("", "fo-pdf-plain-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if err := pdfcpuapi.Write(ctx, tmp, conf); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("pdfcpu write plain: %w", err)
	}
	tmp.Close()
	return tmp.Name(), nil
}

// parsePDFWithLib 使用 ledongthuc/pdf 提取文本，带 60 秒超时保护。
func parsePDFWithLib(filePath string) (text string, err error) {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		t, e := parsePDFWithLibInner(filePath)
		ch <- result{t, e}
	}()
	select {
	case res := <-ch:
		return res.text, res.err
	case <-time.After(60 * time.Second):
		return "", fmt.Errorf("pdf text extraction timeout (possibly image-only PDF)")
	}
}

// parsePDFWithLibInner 实际执行 PDF 文本提取：先尝试 GetPlainText，失败则逐页解析。
func parsePDFWithLibInner(filePath string) (text string, err error) {
	r, openErr := pdf.Open(filePath)
	if openErr != nil {
		return "", fmt.Errorf("open pdf: %w", openErr)
	}

	if reader, e := r.GetPlainText(); e == nil {
		if buf, e2 := io.ReadAll(reader); e2 == nil {
			if t := cleanupText(string(buf)); t != "" {
				return t, nil
			}
		}
	}

	text, err = parsePDFPageByPage(r)
	if err != nil {
		return "", err
	}
	return text, nil
}

// parsePDFPageByPage 逐页提取 PDF 文本内容，带 panic 恢复保护。
func parsePDFPageByPage(r *pdf.Reader) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdf page parse panic: %v", r)
		}
	}()

	var sb strings.Builder
	numPages := r.NumPage()
	for i := 1; i <= numPages; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		for _, t := range p.Content().Text {
			sb.WriteString(t.S)
		}
		sb.WriteByte('\n')
	}
	result = cleanupText(sb.String())
	if result == "" {
		return "", fmt.Errorf("no text extracted (possibly image-only PDF)")
	}
	return result, nil
}
