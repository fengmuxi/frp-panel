package upgrade

import (
	"fmt"
	"net/url"
	"strings"
)

// officialReleaseHosts 允许的发布产物下载源（安全审计 P1-11：显式 DownloadURL
// 曾被原样使用 —— 任意地址 + 无校验 = 节点 root 投毒）。
var officialReleaseHosts = map[string]bool{
	"github.com":                            true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com":  true,
}

// validateOfficialReleaseURL 校验显式提供的下载地址必须是 https 的官方 release 源。
// SHA-256 清单校验（verifyDownloadChecksum）是内容完整性的最终防线。
func validateOfficialReleaseURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid download url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("download url must be https, got [%s]", parsed.Scheme)
	}
	if !officialReleaseHosts[strings.ToLower(parsed.Host)] {
		return fmt.Errorf("download url host [%s] is not an allowed release source", parsed.Host)
	}
	return nil
}

func buildDownloadURL(opt Options) (string, error) {
	if u := strings.TrimSpace(opt.DownloadURL); len(u) > 0 {
		if err := validateOfficialReleaseURL(u); err != nil {
			return "", err
		}
		return u, nil
	}
	version := strings.TrimSpace(opt.Version)
	if len(version) == 0 {
		version = "latest"
	}

	asset, err := detectAssetName()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://github.com/VaalaCat/frp-panel/releases/download/%s/%s", version, asset)
	if opt.UseGithubProxy && len(strings.TrimSpace(opt.GithubProxy)) > 0 {
		url = fmt.Sprintf("%s/%s", strings.TrimRight(strings.TrimSpace(opt.GithubProxy), "/"), url)
	}
	return url, nil
}
