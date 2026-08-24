package parsers

import (
	"fmt"
	"os"
)

// ParseCode 读取代码文件的原始文本内容。
// 代码文件无需特殊解析，直接返回 UTF-8 文本供 StrategyCode 分块器处理。
func ParseCode(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read code file: %w", err)
	}
	return string(data), nil
}
