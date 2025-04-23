package market

import (
	"strings"
)

// IsRetryableError 함수는 재 시도 할 작업인지 검사하는 함수
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// HTTP 관련 일시적 오류인 경우만 재시도
	if strings.Contains(errStr, "HTTP 에러(502)") || // Bad Gateway
		strings.Contains(errStr, "HTTP 에러(503)") || // Service Unavailable
		strings.Contains(errStr, "HTTP 에러(504)") || // Gateway Timeout
		strings.Contains(errStr, "i/o timeout") || // 타임아웃
		strings.Contains(errStr, "connection refused") || // 연결 거부
		strings.Contains(errStr, "EOF") || // 예기치 않은 연결 종료
		strings.Contains(errStr, "no such host") { // DNS 해석 실패
		return true
	}

	return false
}
