package upgrade

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/VaalaCat/frp-panel/utils"
)

// checksum.go 下载产物的完整性校验（安全审计 P1-11）：
// 此前升级只验证机器类型（ELF/PE），租户可控的下载地址可以把任意二进制
// 推到节点上以 root 替换运行。现在强制下载 release 旁挂的 <url>.sha256
// 清单并比对 —— 发布流水线（tag workflow / build.sh）负责生成该文件。

// verifyDownloadChecksum 下载 <downloadURL>.sha256 并与产物实际哈希比对。
// 清单缺失或哈希不一致都视为升级失败（fail-closed）。
func verifyDownloadChecksum(ctx context.Context, binaryPath, downloadURL string) error {
	sumFile, err := utils.DownloadFile(ctx, downloadURL+".sha256", "")
	if err != nil {
		return fmt.Errorf("checksum manifest [%s.sha256] unavailable, refusing upgrade: %w", downloadURL, err)
	}

	raw, err := os.ReadFile(sumFile)
	if err != nil {
		return fmt.Errorf("read checksum manifest: %w", err)
	}
	expected, err := parseChecksumLine(string(raw))
	if err != nil {
		return fmt.Errorf("parse checksum manifest: %w", err)
	}

	actualBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read staged binary: %w", err)
	}
	actualHash := sha256.Sum256(actualBytes)
	actual := hex.EncodeToString(actualHash[:])

	if subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
		return fmt.Errorf("checksum mismatch: manifest=%s actual=%s, refusing upgrade", expected, actual)
	}
	return nil
}

// parseChecksumLine 兼容两种清单格式：纯哈希行，以及 `sha256sum` 风格的 "<hash>  <filename>"。
func parseChecksumLine(raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	for _, f := range fields {
		f = strings.ToLower(strings.TrimSpace(f))
		if len(f) == 64 && isHex(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("no 64-hex checksum found in manifest")
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
