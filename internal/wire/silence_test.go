package wire

import (
	"testing"

	header "github.com/freequasar/quasar-go-genproto/proto/alice/protos/api/alicekit/events/common/event_header"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestSetUtteranceSilenceReplacesRecognizerLimit(t *testing.T) {
	h, _ := proto.Marshal(&header.TEventHeader{MessageId: "silence-test"})
	asr := protowire.AppendVarint(protowire.AppendTag(nil, 5, protowire.VarintType), 400)
	voice := fieldBytes(fieldBytes(nil, 13, []byte("keep")), 25, asr)
	event := fieldBytes(fieldBytes(nil, 1, h), 3, voice)
	raw := append([]byte("AAPI"), fieldBytes(nil, 1, event)...)
	got, err := SetUtteranceSilence(Decode(raw, ClientToServer), 4000)
	if err != nil {
		t.Fatal(err)
	}
	parsed := Decode(got, ClientToServer)
	if v := parsed.Client.GetEvent().GetVoiceInput().GetAdvancedAsrOptions().GetUtteranceSilence(); v != 4000 {
		t.Fatalf("silence = %d", v)
	}
	if _, err := SetUtteranceSilence(Decode(raw, ClientToServer), 1); err != nil {
		t.Fatal(err)
	}
}
