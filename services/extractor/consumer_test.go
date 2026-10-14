package main

import (
	"math"
	"testing"

	"github.com/streadway/amqp"
)

func TestRetryCountBoundsUntrustedHeaderValues(t *testing.T) {
	tests := []struct {
		name    string
		headers amqp.Table
		want    int
	}{
		{name: "missing", headers: nil, want: 0},
		{name: "valid", headers: amqp.Table{"x-retry-count": int32(2)}, want: 2},
		{name: "negative", headers: amqp.Table{"x-retry-count": int64(-1)}, want: 0},
		{name: "too large", headers: amqp.Table{"x-retry-count": int64(math.MaxInt64)}, want: maxRetries + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryCount(test.headers); got != test.want {
				t.Errorf("retryCount() = %d; want %d", got, test.want)
			}
		})
	}
}
