package utils

import (
	"testing"

	"github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
)

func TestLoadConfigureFromContent(t *testing.T) {
	content := []byte(`[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000`)

	allCfg := &v1.ClientConfig{}

	if err := LoadConfigureFromContent(content, allCfg, true); err != nil {
		t.Error(err)
	}
	t.Logf("%+v", allCfg)
}

// TestLoadClientConfigCompleteBeforeValidate 复现并固化修复语义：
// 前端提交的 proxy 配置不含 bandwidthLimitMode（空字符串），frp 的校验器
// 要求该字段必须为 client/server，默认值 "client" 由 Complete 填充。
// 因此任何"校验存储配置"的路径都必须先 Complete 再 Validate，
// 否则会误报 "proxy xxx: bandwidth limit mode should be client or server"。
func TestLoadClientConfigCompleteBeforeValidate(t *testing.T) {
	content := []byte(`{
	"proxies": [
		{
			"name": "test",
			"type": "tcp",
			"localPort": 22,
			"remotePort": 6000
		}
	]
}`)

	// 未 Complete 的原始 proxies（LoadConfigureFromContent 不做默认值填充）
	allCfg := &v1.ClientConfig{}
	if err := LoadConfigureFromContent(content, allCfg, false); err != nil {
		t.Fatalf("load raw config: %v", err)
	}
	var incompleteProxies []v1.ProxyConfigurer
	for _, p := range allCfg.Proxies {
		incompleteProxies = append(incompleteProxies, p.ProxyConfigurer)
	}

	cliCfg := &allCfg.ClientCommonConfig
	cliCfg.Complete()

	// 反向固化根因：跳过 Complete 直接校验，空 bandwidthLimitMode 必须报错
	_, err := validation.ValidateAllClientConfig(cliCfg, incompleteProxies, nil)
	if err == nil {
		t.Fatalf("validate without complete should fail for empty bandwidthLimitMode")
	}
	t.Logf("expected validation error without complete: %v", err)

	// frp 官方流程：Complete 之后再校验，空 bandwidthLimitMode 被补为 client
	for _, p := range incompleteProxies {
		p.Complete("")
	}
	for _, p := range incompleteProxies {
		if p.GetBaseConfig().Transport.BandwidthLimitMode != "client" {
			t.Fatalf("bandwidth limit mode should be completed to client, got %q",
				p.GetBaseConfig().Transport.BandwidthLimitMode)
		}
		if p.GetBaseConfig().Name != "test" {
			t.Fatalf(`Complete("") must not change proxy name, got %q`, p.GetBaseConfig().Name)
		}
	}

	warning, err := validation.ValidateAllClientConfig(cliCfg, incompleteProxies, nil)
	if err != nil {
		t.Fatalf("validate after complete should pass: %v (warning: %v)", err, warning)
	}
}
