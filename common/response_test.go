package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VaalaCat/frp-panel/defs"
	"github.com/gin-gonic/gin"
)

// 会话失效必须能以 code 401 被前端识别（2026-11-24 修复"登录过期不跳登录页"）：
// 原实现 JSON 分支走 Err() 回 code 500，与"服务内部错误"无法区分，
// 前端拦截器无法据此清态跳转。protobuf 分支仍回 HTTP 401 状态码。
func TestErrUnAuthorizedJSONReturnsCode401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/user/get", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	ErrUnAuthorized(c, "token invalid")

	if rec.Code != http.StatusOK {
		t.Fatalf("JSON 分支应回 HTTP 200（body 内 code=401），实际 %d", rec.Code)
	}
	var got Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是 JSON: %v, body=%s", err, rec.Body.String())
	}
	if got.Code != 401 {
		t.Fatalf("会话失效 body code 应为 401，实际 %d（body=%s）", got.Code, rec.Body.String())
	}
	if got.Msg != "token invalid" {
		t.Fatalf("msg 应保留原因，实际 %q", got.Msg)
	}
}

func TestErrUnAuthorizedProtoReturnsHTTP401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/user/get", nil)
	c.Request.Header.Set("Content-Type", "application/x-protobuf")

	ErrUnAuthorized(c, "token invalid")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("protobuf 分支应回 HTTP 401，实际 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-protobuf" {
		t.Fatalf("protobuf 分支 Content-Type 异常: %q", ct)
	}
}

// 成功响应保持 code 200，避免误把 OK 当会话失效触发跳转
func TestOKRespCode200(t *testing.T) {
	if OK("").Code != 200 {
		t.Fatalf("OK 应为 code 200，实际 %d", OK("").Code)
	}
	if UnAuth("").Code != 401 {
		t.Fatalf("UnAuth 应为 code 401，实际 %d", UnAuth("").Code)
	}
	if Err("").Code != 500 {
		t.Fatalf("Err 应为 code 500，实际 %d", Err("").Code)
	}
	if defs.ReqSuccess != "success" {
		t.Fatalf("ReqSuccess 常量被改动: %v", defs.ReqSuccess)
	}
}
