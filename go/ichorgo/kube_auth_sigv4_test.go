package ichorgo

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The worked example of the AWS Signature Version 4 documentation (IAM ListUsers).
func TestSigV4DocumentationExample(t *testing.T) {
	key := sigV4Key("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "20150830", "us-east-1", "iam")
	if got := hex.EncodeToString(key); got != "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9" {
		t.Fatalf("signing key %s", got)
	}

	req, _ := http.NewRequest(http.MethodGet, "https://iam.amazonaws.com/?Action=ListUsers&Version=2010-05-08", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	now, _ := time.Parse(sigV4TimeFormat, "20150830T123600Z")
	sigV4Sign(req, emptySHA256, awsCredentials{accessKeyID: "AKIDEXAMPLE", secretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}, "us-east-1", "iam", now)

	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/iam/aws4_request, SignedHeaders=content-type;host;x-amz-date, Signature=5d672d79c15b13162d9279b0855cfba6789a8edb4c82c400e06b5924a6f2b5d7"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
}

// The presigned-URL example of the S3 SigV4 documentation (GET test.txt, 86400 s).
func TestSigV4PresignDocumentationExample(t *testing.T) {
	u, _ := url.Parse("https://examplebucket.s3.amazonaws.com/test.txt")
	now, _ := time.Parse(sigV4TimeFormat, "20130524T000000Z")

	got := sigV4Presign(u, http.Header{}, awsCredentials{accessKeyID: "AKIAIOSFODNN7EXAMPLE", secretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}, "us-east-1", "s3", "UNSIGNED-PAYLOAD", 86400*time.Second, now)

	if !strings.Contains(got, "X-Amz-Expires=86400") || !strings.Contains(got, "X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404") {
		t.Fatalf("presigned %s", got)
	}
}
