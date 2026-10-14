package rabbitmq

import (
	"testing"

	"github.com/streadway/amqp"
)

func TestRetryCountBoundsHeaderValues(t *testing.T) {
	tests := []struct {
		name    string
		headers amqp.Table
		want    int
	}{
		{name: "nil headers"},
		{name: "missing header", headers: amqp.Table{}},
		{name: "int", headers: amqp.Table{"x-retry-count": int(2)}, want: 2},
		{name: "int32", headers: amqp.Table{"x-retry-count": int32(3)}, want: 3},
		{name: "int64", headers: amqp.Table{"x-retry-count": int64(4)}, want: 4},
		{name: "negative", headers: amqp.Table{"x-retry-count": int64(-2)}},
		{name: "unexpected type", headers: amqp.Table{"x-retry-count": "4"}},
		{name: "beyond retry limit", headers: amqp.Table{"x-retry-count": int64(99)}, want: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RetryCount(test.headers, 3); got != test.want {
				t.Fatalf("RetryCount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCopyHeadersDoesNotMutateSource(t *testing.T) {
	source := amqp.Table{"existing": "value"}
	copied := CopyHeaders(source)
	copied["new"] = "added"
	if _, exists := source["new"]; exists {
		t.Fatal("CopyHeaders() returned a map sharing the source map")
	}
	if copied["existing"] != source["existing"] {
		t.Fatalf("copied existing value = %v, want %v", copied["existing"], source["existing"])
	}
}
