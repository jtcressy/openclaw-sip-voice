package callflow

import (
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestDiagoDialerIdentityHeadersUseUsernameAndExtension(t *testing.T) {
	dialer := DiagoDialer{DiagoDialerOptions: DiagoDialerOptions{
		Username:  "openclaw-line",
		Extension: "4321",
	}}

	headers := dialer.identityHeaders("192.168.20.1")
	if len(headers) != 1 {
		t.Fatalf("headers = %d, want 1", len(headers))
	}
	from, ok := headers[0].(*sip.FromHeader)
	if !ok {
		t.Fatalf("header type = %T, want *sip.FromHeader", headers[0])
	}
	if from.DisplayName != "4321" {
		t.Fatalf("display name = %q, want extension", from.DisplayName)
	}
	if from.Address.User != "openclaw-line" {
		t.Fatalf("from user = %q, want SIP username", from.Address.User)
	}
	if from.Address.Host != "192.168.20.1" {
		t.Fatalf("from host = %q, want SIP server host", from.Address.Host)
	}
}

func TestDiagoDialerIdentityHeadersFallsBackToUsernameDisplay(t *testing.T) {
	dialer := DiagoDialer{DiagoDialerOptions: DiagoDialerOptions{
		Username: "openclaw-line",
	}}

	headers := dialer.identityHeaders("192.168.20.1")
	from, ok := headers[0].(*sip.FromHeader)
	if !ok {
		t.Fatalf("header type = %T, want *sip.FromHeader", headers[0])
	}
	if from.DisplayName != "openclaw-line" {
		t.Fatalf("display name = %q, want SIP username fallback", from.DisplayName)
	}
}
