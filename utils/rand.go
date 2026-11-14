package utils

import (
	"math/rand"
	"strings"
)

func RandomInt(a, b int) int {
	if a > b {
		a, b = b, a
	}
	return rand.Intn(b-a+1) + a
}

// RandomString 生成指定长度的随机字符串（字母数字），用于自动生成节点 ID 后缀
func RandomString(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		sb.WriteByte(charset[rand.Intn(len(charset))])
	}
	return sb.String()
}
