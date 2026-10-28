package httpapi

import "net/http"

// RequestMeter counts requests. Record must not block the caller on I/O.
type RequestMeter interface {
	Record()
}

// MeteringMiddleware counts every request that reaches the API surface. It sits
// outside the router's rate limiter and CSRF check, so a rejected request still
// counts: the metric describes traffic received, not traffic served.
func MeteringMiddleware(meter RequestMeter, next http.Handler) http.Handler {
	if meter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meter.Record()
		next.ServeHTTP(w, r)
	})
}
