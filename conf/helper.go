package conf

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/VaalaCat/frp-panel/utils"
	"github.com/VaalaCat/frp-panel/utils/logger"
	v1 "github.com/fatedier/frp/pkg/config/v1"
)

// listenIP 兜底：监听 IP 未配置（空串）时按 0.0.0.0 监听所有网卡（与历史行为一致）；
// 配置了具体 IP（如 127.0.0.1）则只绑定该地址
func listenIP(cfgIP string) string {
	ip := strings.TrimSpace(cfgIP)
	if ip == "" {
		return "0.0.0.0"
	}
	return ip
}

// RPCListenAddr 节点 RPC（wsgrpc）监听地址：IP 由 MASTER_RPC_LISTEN_IP 控制，
// 默认 0.0.0.0 监听所有网卡；节点全走反代时可改 127.0.0.1 收紧
func RPCListenAddr(cfg Config) string {
	return fmt.Sprintf("%s:%d", listenIP(cfg.Master.RPCListenIP), cfg.Master.RPCPort)
}

func rpcCallAddr(cfg Config) string {
	return fmt.Sprintf("%s:%d", cfg.Master.RPCHost, cfg.Master.RPCPort)
}

// localDialHost 进程内回环拨号主机：监听 0.0.0.0/空 → 127.0.0.1（包不出网卡，
// 不经过云 NAT/防火墙，规避自拨公网地址 hairpin 不通导致的超时）；
// 配置了具体监听 IP 则直接使用它（该地址必然本机可达）
func localDialHost(cfgIP string) string {
	host := listenIP(cfgIP)
	if host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

// LocalRPCDialURL 内置默认服务端（与 master 同进程）拨号用的回环地址。
// CLIENT_RPC_URL 的语义是「外部节点视角访问 master 的地址」（CDN/反代域名或公网 IP），
// 用作进程内拨号会因云 NAT 不支持 hairpin / CDN 未放行 9001 而超时，故强制回环。
func LocalRPCDialURL(cfg Config) string {
	return fmt.Sprintf("grpc://%s:%d", localDialHost(cfg.Master.RPCListenIP), cfg.Master.RPCPort)
}

// LocalAPIDialURL 内置服务端进程内调 master REST API 用的回环地址，理由同上
func LocalAPIDialURL(cfg Config) string {
	return fmt.Sprintf("http://%s:%d", localDialHost(cfg.Master.APIListenIP), cfg.Master.APIPort)
}

func JWTSecret(cfg Config) string {
	return utils.SHA1(fmt.Sprintf("%s:%d:%s", cfg.Master.APIHost, cfg.Master.APIPort, cfg.App.GlobalSecret))
}

// MasterAPIListenAddr 面板 API/WebUI 监听地址：IP 由 MASTER_API_LISTEN_IP 控制，
// 默认 0.0.0.0 监听所有网卡；仅本机反代访问时可改 127.0.0.1 收紧
func MasterAPIListenAddr(cfg Config) string {
	return fmt.Sprintf("%s:%d", listenIP(cfg.Master.APIListenIP), cfg.Master.APIPort)
}

func ServerAPIListenAddr(cfg Config) string {
	// 安全审计 P1-9：server API 仅承载本机 frps 的 /auth 插件回源（宿主固定 127.0.0.1），
	// 默认绑定回环地址，不再暴露公网
	return fmt.Sprintf("127.0.0.1:%d", cfg.Server.APIPort)
}

func FRPsAuthOption(cfg Config) v1.HTTPPluginOptions {
	authUrl := fmt.Sprintf("http://%s:%d/auth", defs.LocalHost, cfg.Server.APIPort)
	parsedUrl, err := url.Parse(authUrl)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Fatalf("parse auth url error")
	}

	return v1.HTTPPluginOptions{
		Name: defs.FRP_Plugin_Multiuser,
		Ops:  []string{"Login"},
		Addr: parsedUrl.Host,
		Path: parsedUrl.Path,
	}
}

func GetJWTWithPayload(cfg Config, uid int, payload map[string]interface{}) (string, error) {
	payload[defs.UserIDKey] = uid
	// jti（安全审计 P2）：登出/改密时可用其做服务端吊销
	payload["jti"] = utils.GenerateUUID()
	return utils.GetJwtTokenFromMap(JWTSecret(cfg),
		time.Now().Unix(),
		int64(cfg.App.CookieAge),
		payload)
}

// GetJWTWithPayloadAndExpiry 以指定有效期签发 JWT（供续签场景使用：
// 续签必须沿用原 token 的过期时间，不得滑动延长 —— 安全审计 P2）。
// payload 中已有的 jti 会保留，用于吊销链路。
func GetJWTWithPayloadAndExpiry(cfg Config, uid int, payload map[string]interface{}, expirySeconds int64) (string, error) {
	payload[defs.UserIDKey] = uid
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if jti, _ := payload["jti"].(string); jti == "" {
		payload["jti"] = utils.GenerateUUID()
	}
	return utils.GetJwtTokenFromMap(JWTSecret(cfg),
		time.Now().Unix(),
		expirySeconds,
		payload)
}

func GetJWTWithAllPermission(cfg Config, uid int) string {
	token, _ := GetJWTWithPayload(cfg, uid, map[string]interface{}{
		defs.TokenPayloadKey_Permissions: AllPermission(),
	})
	return token
}

func AllPermission() []defs.APIPermission {
	return []defs.APIPermission{{
		Method: "*",
		Path:   "*",
	}}
}

func GetCommonJWT(cfg Config, uid int) string {
	token, _ := GetJWTWithPayload(cfg, uid, map[string]interface{}{})
	return token
}

func GetCommonJWTWithExpireTime(cfg Config, uid int64, expSec int) string {
	token, _ := utils.GetJwtTokenFromMap(JWTSecret(cfg),
		time.Now().Unix(),
		int64(expSec),
		map[string]interface{}{defs.UserIDKey: uid})
	return token
}

func GetAPIURL(cfg Config) string {

	if len(cfg.Client.APIUrl) != 0 {
		return cfg.Client.APIUrl
	}

	return fmt.Sprintf("%s://%s:%d", cfg.Master.APIScheme, cfg.Master.APIHost, cfg.Master.APIPort)
}

func GetCertTemplate(cfg Config) *x509.Certificate {
	now := time.Now()
	return &x509.Certificate{
		SerialNumber: big.NewInt(now.Unix()),
		Subject: pkix.Name{
			CommonName:         cfg.Master.APIHost,
			Country:            []string{"CN"},
			Organization:       []string{"frp-panel"},
			OrganizationalUnit: []string{"frp-panel"},
		},
		SignatureAlgorithm: x509.SHA512WithRSA,
		DNSNames:           []string{cfg.Master.APIHost},
		IPAddresses:        []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:          now,
		// 自签 CA 有效期 3 年（安全审计 P3：原 10 年过长，私钥入库泄露半径大；
		// 轮换方式见 docs/deploy-master.md「证书轮换」）
		NotAfter:              now.AddDate(3, 0, 0),
		SubjectKeyId:          []byte{102, 114, 112, 45, 112, 97, 110, 101, 108},
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		KeyUsage: x509.KeyUsageKeyEncipherment |
			x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageKeyAgreement |
			x509.KeyUsageDataEncipherment,
	}
}

type LisOpt struct {
	MuxLis net.Listener
	ApiLis net.Listener
	RunAPI bool
}

func GetListener(c context.Context, cfg Config) LisOpt {
	runAPI := RPCListenAddr(cfg) != MasterAPIListenAddr(cfg)

	muxLis, err := net.Listen("tcp", RPCListenAddr(cfg))
	if err != nil {
		logger.Logger(c).WithError(err).Fatalf("failed to listen: %v", RPCListenAddr(cfg))
	}

	opt := LisOpt{
		MuxLis: muxLis,
		RunAPI: runAPI,
	}

	if runAPI {
		apiLis, err := net.Listen("tcp", MasterAPIListenAddr(cfg))
		if err != nil {
			logger.Logger(c).WithError(err).Warnf("failed to listen: %v, but mux server can handle http api", MasterAPIListenAddr(cfg))
		}
		opt.ApiLis = apiLis
	}

	if !runAPI {
		opt.ApiLis = nil
	}

	return opt
}

type ConnInfo struct {
	Host   string
	Scheme Scheme
}

type Scheme string

const (
	GRPC Scheme = "grpc"
	WS   Scheme = "ws"
	WSS  Scheme = "wss"
)

func GetRPCConnInfo(cfg Config) ConnInfo {
	rpcUrl := cfg.Client.RPCUrl

	if len(rpcUrl) == 0 {
		return ConnInfo{
			Host:   rpcCallAddr(cfg),
			Scheme: GRPC,
		}
	}

	parsedUrl, err := url.Parse(rpcUrl)
	if err != nil {
		logger.Logger(context.Background()).WithError(err).Fatalf("parse rpc url error")
	}

	return ConnInfo{
		Host:   parsedUrl.Host,
		Scheme: Scheme(parsedUrl.Scheme),
	}
}
