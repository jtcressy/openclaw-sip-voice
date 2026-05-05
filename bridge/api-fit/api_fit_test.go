package apifit

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/diago/media"
	"github.com/emiago/diago/media/sdp"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

func TestContactAndSDPAdvertisedIPCanUseMacvlanIP(t *testing.T) {
	const macvlanIP = "198.51.100.23"

	ua, _, _, err := NewSpikeUA(macvlanIP)
	if err != nil {
		t.Fatalf("new spike UA: %v", err)
	}
	defer ua.Close()

	dg := NewSpikeDiago(ua, macvlanIP)
	tx, err := NewRegisterTransaction(context.Background(), dg, "registrar.example.test", "alice", "not-a-secret", 30*time.Second)
	if err != nil {
		t.Fatalf("register transaction: %v", err)
	}

	contact := tx.Origin.Contact()
	if contact == nil {
		t.Fatal("REGISTER transaction did not build Contact header")
	}
	if got := contact.Address.Host; got != macvlanIP {
		t.Fatalf("Contact host = %q, want %q", got, macvlanIP)
	}
	if got := contact.Address.Port; got != SpikeSIPPort {
		t.Fatalf("Contact port = %d, want %d", got, SpikeSIPPort)
	}

	offer := BuildLocalPCMUOffer("0.0.0.0", macvlanIP, 40000)
	if !bytes.Contains(offer, []byte("c=IN IP4 "+macvlanIP)) {
		t.Fatalf("SDP did not advertise macvlan IP; SDP:\n%s", offer)
	}

	parsed := sdp.SessionDescription{}
	if err := sdp.Unmarshal(offer, &parsed); err != nil {
		t.Fatalf("parse SDP: %v", err)
	}
	conn, err := parsed.ConnectionInformation()
	if err != nil {
		t.Fatalf("SDP connection info: %v", err)
	}
	if !conn.IP.Equal(net.ParseIP(macvlanIP)) {
		t.Fatalf("SDP connection IP = %s, want %s", conn.IP, macvlanIP)
	}
}

func TestEncodedPCMU20msRTPReaderWriterSurfaces(t *testing.T) {
	codec := media.CodecAudioUlaw
	if codec.PayloadType != 0 {
		t.Fatalf("PCMU payload type = %d, want 0", codec.PayloadType)
	}
	if codec.SampleDur != 20*time.Millisecond {
		t.Fatalf("PCMU sample duration = %s, want 20ms", codec.SampleDur)
	}
	if codec.SampleTimestamp() != 160 {
		t.Fatalf("PCMU RTP timestamp step = %d, want 160", codec.SampleTimestamp())
	}

	payload := bytes.Repeat([]byte{0xff}, codec.SamplesPCM(8))
	if len(payload) != 160 {
		t.Fatalf("20ms PCMU frame size = %d, want 160", len(payload))
	}

	source := &singleRTPReader{payload: payload}
	sink := &capturingRTPWriter{}
	encodedReader, encodedWriter, packetReader, packetWriter := NewPCMUEncodedSurfaces(source, sink)

	var _ io.Reader = encodedReader
	var _ io.Writer = encodedWriter

	readBuf := make([]byte, len(payload))
	n, err := encodedReader.Read(readBuf)
	if err != nil {
		t.Fatalf("read encoded RTP payload: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("read bytes = %d, want %d", n, len(payload))
	}
	if !bytes.Equal(readBuf, payload) {
		t.Fatal("decoded RTP payload reader did not expose encoded PCMU bytes unchanged")
	}
	if packetReader.PacketHeader.PayloadType != codec.PayloadType {
		t.Fatalf("read payload type = %d, want %d", packetReader.PacketHeader.PayloadType, codec.PayloadType)
	}

	n, err = WritePCMU20ms(packetWriter, payload, true)
	if err != nil {
		t.Fatalf("write encoded RTP payload: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("written bytes = %d, want %d", n, len(payload))
	}
	if sink.last.Header.PayloadType != codec.PayloadType {
		t.Fatalf("written payload type = %d, want %d", sink.last.Header.PayloadType, codec.PayloadType)
	}
	if sink.last.Header.Timestamp != 0 {
		t.Fatalf("first written timestamp = %d, want 0", sink.last.Header.Timestamp)
	}
	if !bytes.Equal(sink.last.Payload, payload) {
		t.Fatal("RTP writer did not packetize PCMU payload unchanged")
	}

	n, err = WritePCMU20ms(packetWriter, payload, false)
	if err != nil {
		t.Fatalf("write second encoded RTP payload: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("second written bytes = %d, want %d", n, len(payload))
	}
	if sink.last.Header.Timestamp != codec.SampleTimestamp() {
		t.Fatalf("second written timestamp = %d, want %d", sink.last.Header.Timestamp, codec.SampleTimestamp())
	}
}

func TestInboundHandlerRegistrationAndOutboundDialogConstructionCompile(t *testing.T) {
	ua, _, server, err := NewSpikeUA("127.0.0.1")
	if err != nil {
		t.Fatalf("new spike UA: %v", err)
	}
	defer ua.Close()

	seen := map[sip.RequestMethod]bool{}
	RegisterInboundHandlers(server, func(req *sip.Request, tx sip.ServerTransaction) {
		seen[req.Method] = true
	})

	methods := strings.Join(server.RegisteredMethods(), ",")
	for _, method := range []string{sip.INVITE.String(), sip.ACK.String(), sip.BYE.String(), sip.CANCEL.String()} {
		if !strings.Contains(methods, method) {
			t.Fatalf("server methods %q did not include %s", methods, method)
		}
	}

	dg := NewSpikeDiago(ua, "127.0.0.1")
	dialog, err := BuildOutboundCall(dg, "bob", "pbx.example.test")
	if err != nil {
		t.Fatalf("build outbound dialog: %v", err)
	}
	if dialog.InviteRequest.Method != sip.INVITE {
		t.Fatalf("outbound dialog method = %s, want INVITE", dialog.InviteRequest.Method)
	}
}

type singleRTPReader struct {
	payload []byte
	sent    bool
}

func (r *singleRTPReader) ReadRTP(_ []byte, p *rtp.Packet) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	p.Header = rtp.Header{
		Version:        2,
		PayloadType:    media.CodecAudioUlaw.PayloadType,
		SequenceNumber: 1,
		Timestamp:      media.CodecAudioUlaw.SampleTimestamp(),
		SSRC:           42,
	}
	copy(p.Payload, r.payload)
	return p.Header.MarshalSize() + len(r.payload), nil
}

type capturingRTPWriter struct {
	last rtp.Packet
}

func (w *capturingRTPWriter) WriteRTP(p *rtp.Packet) error {
	w.last.Header = p.Header
	w.last.Payload = append(w.last.Payload[:0], p.Payload...)
	return nil
}
