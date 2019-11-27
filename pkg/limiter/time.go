package limiter

import (
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
)

func timeUnitToWindowSize(unit string) int64 {
	switch unit {
	case "second":
		return 1
	case "minute":
		return 60
	case "hour":
		return 60 * 60
	case "day":
		return 60 * 60 * 24
	}

	panic("should not get here")
}

func timeUnitToPb(unit string) pb.RateLimitResponse_RateLimit_Unit {
	switch unit {
	case "second":
		return pb.RateLimitResponse_RateLimit_SECOND
	case "minute":
		return pb.RateLimitResponse_RateLimit_MINUTE
	case "hour":
		return pb.RateLimitResponse_RateLimit_HOUR
	case "day":
		return pb.RateLimitResponse_RateLimit_DAY
	default:
		return pb.RateLimitResponse_RateLimit_UNKNOWN
	}
}
