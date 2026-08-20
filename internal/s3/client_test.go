package s3

import "testing"

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{"valid", "s3://my-bucket/path/to/object.txt", "my-bucket", "path/to/object.txt", false},
		{"valid single-segment key", "s3://bucket/key", "bucket", "key", false},
		{"missing scheme", "my-bucket/key", "", "", true},
		{"missing key", "s3://bucket-only", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := parseS3URI(tt.uri)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if bucket != tt.wantBucket || key != tt.wantKey {
				t.Errorf("got (%q, %q), want (%q, %q)", bucket, key, tt.wantBucket, tt.wantKey)
			}
		})
	}
}

func TestParseS3URIExportedWrapper(t *testing.T) {
	bucket, key, err := ParseS3URI("s3://b/k")
	if err != nil || bucket != "b" || key != "k" {
		t.Errorf("ParseS3URI = (%q, %q, %v)", bucket, key, err)
	}
}

func TestGetObjectURL(t *testing.T) {
	c := &Client{region: "us-west-2"}
	got := c.GetObjectURL("my-bucket", "dir/file name.txt")
	want := "https://my-bucket.s3.us-west-2.amazonaws.com/dir%2Ffile%20name.txt"
	if got != want {
		t.Errorf("GetObjectURL = %q, want %q", got, want)
	}
}
