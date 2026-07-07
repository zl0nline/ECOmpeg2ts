package input

import "testing"

func TestParseMulticastUDPURLWithAt(t *testing.T) {
	spec, err := ParseSource("udp://@239.3.1.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Scheme != "udp" {
		t.Fatalf("scheme = %q, want udp", spec.Scheme)
	}
	if spec.Address != "239.3.1.1:1234" {
		t.Fatalf("address = %q, want 239.3.1.1:1234", spec.Address)
	}
	if !spec.IsMulticast {
		t.Fatal("expected multicast source")
	}
}

func TestParseUnicastUDPURL(t *testing.T) {
	spec, err := ParseSource("udp://0.0.0.0:1234")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IsMulticast {
		t.Fatal("did not expect multicast source")
	}
}

func TestParseFileFallback(t *testing.T) {
	spec, err := ParseSource("sample.ts")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Scheme != "file" || spec.FilePath != "sample.ts" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}
