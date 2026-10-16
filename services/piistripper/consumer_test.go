package main

import (
	"testing"
)

func TestValidJob(t *testing.T) {
	const bucket = "resumes"

	tests := []struct {
		name string
		job  redactionJob
		want bool
	}{
		{
			name: "valid job",
			job: redactionJob{
				ID:        "12345678-1234-1234-1234-123456789abc",
				Bucket:    bucket,
				ObjectKey: "results/12345678-1234-1234-1234-123456789abc.txt",
			},
			want: true,
		},
		{
			name: "wrong bucket",
			job: redactionJob{
				ID:        "12345678-1234-1234-1234-123456789abc",
				Bucket:    "other-bucket",
				ObjectKey: "results/12345678-1234-1234-1234-123456789abc.txt",
			},
			want: false,
		},
		{
			name: "invalid job ID",
			job: redactionJob{
				ID:        "not-a-uuid",
				Bucket:    bucket,
				ObjectKey: "results/not-a-uuid.txt",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validJob(tt.job, bucket); got != tt.want {
				t.Fatalf("validJob() = %v, want %v", got, tt.want)
			}
		})
	}
}
