package checker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fengmuxi/frp-panel/defs"
	"google.golang.org/protobuf/proto"
)

// PostProto 以项目统一的 protobuf 协议发起 POST：
// Content-Type: application/x-protobuf，Body 为 proto.Marshal 结果。
// 可选携带 JWT（与 defs.AuthorizationKey 约定一致，无需 Bearer 前缀，
// 项目的 JWTAuth 中间件直接解析 header 原值）。
func (e *Env) PostProto(ctx context.Context, path string, msg proto.Message, token ...string) (int, http.Header, []byte, error) {
	raw, err := proto.Marshal(msg)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("marshal %T: %w", msg, err)
	}
	return e.postRaw(ctx, path, "application/x-protobuf", raw, token...)
}

// PostJSON 发起 JSON POST（/auth 插件端点等 JSON 接口使用）。
func (e *Env) PostJSON(ctx context.Context, path, body string, token ...string) (int, http.Header, []byte, error) {
	return e.postRaw(ctx, path, "application/json", []byte(body), token...)
}

// Get 发起 GET 请求。
func (e *Env) Get(ctx context.Context, path string, token ...string) (int, http.Header, []byte, error) {
	return e.do(ctx, http.MethodGet, path, "", nil, token...)
}

func (e *Env) postRaw(ctx context.Context, path, contentType string, body []byte, token ...string) (int, http.Header, []byte, error) {
	return e.do(ctx, http.MethodPost, path, contentType, body, token...)
}

func (e *Env) do(ctx context.Context, method, path, contentType string, body []byte, token ...string) (int, http.Header, []byte, error) {
	url := path
	if !strings.Contains(path, "://") {
		url = e.APIBase() + path
	}

	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build request %s %s: %w", method, url, err)
	}
	if contentType != "" {
		req.Header.Set(defs.ContentTypeKey, contentType)
	}
	if len(token) > 0 && token[0] != "" {
		req.Header.Set(defs.AuthorizationKey, token[0])
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB 上限，防止大响应拖垮 check
	if err != nil {
		return resp.StatusCode, resp.Header, nil, fmt.Errorf("read body %s: %w", url, err)
	}
	return resp.StatusCode, resp.Header, data, nil
}

// ProtoStatusBody 判断响应体是否为 protobuf 编码。
func ProtoStatusBody(h http.Header) bool {
	return strings.Contains(h.Get(defs.ContentTypeKey), "protobuf")
}
