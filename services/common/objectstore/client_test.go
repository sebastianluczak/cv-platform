package objectstore

import "testing"

func TestConfigFromEnvUsesDefaultsAndTrimsValues(t *testing.T) {
	values := map[string]string{
		"RUSTFS_REGION":            " eu-central-1 ",
		"RUSTFS_ACCESS_KEY_ID":     " key ",
		"RUSTFS_SECRET_ACCESS_KEY": " secret ",
		"RUSTFS_ENDPOINT_URL":      " http://object-store:9000 ",
		"RUSTFS_BUCKET":            " resumes ",
	}
	config := configFromEnv(func(name string) string {
		return values[name]
	})
	if config.Region != "eu-central-1" ||
		config.AccessKeyID != "key" ||
		config.SecretAccessKey != "secret" ||
		config.Endpoint != "http://object-store:9000" ||
		config.Bucket != "resumes" {
		t.Fatalf("unexpected config: %+v", config)
	}

	config = configFromEnv(func(string) string { return "  " })
	if config.Region != "us-east-1" ||
		config.AccessKeyID != "rustfsadmin" ||
		config.SecretAccessKey != "rustfsadmin" ||
		config.Endpoint != "http://localhost:9000" ||
		config.Bucket != "cv-platform-uploads" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}
